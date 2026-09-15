package container

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// maxCopyFileBytes is the maximum size of a single labeled file to snapshot.
	maxCopyFileBytes = 1 << 20
	// maxCopyFileTarBytes bounds the tar stream read for a labeled file.
	maxCopyFileTarBytes = maxCopyFileBytes + 32<<10
	// maxCopyFileStoreBytes caps in-flight snapshot archives across all containers.
	maxCopyFileStoreBytes = 16 << 20
	// unixFilePermMask keeps owner/group/other bits and drops setuid, setgid, and sticky.
	unixFilePermMask = 0o777
	// bindStringMinParts is source and destination in a HostConfig.Binds entry.
	bindStringMinParts = 2
)

// CopyFileStore captures labeled files around recreate and discards leftovers.
type CopyFileStore interface {
	// SnapshotCopyFiles captures labeled files before the source is removed.
	SnapshotCopyFiles(ctx context.Context, container types.Container) error
	// DiscardCopyFiles drops a leftover snapshot for the source container.
	DiscardCopyFiles(containerID types.ContainerID)
}

var _ CopyFileStore = (*client)(nil)

// ArchiveAPI is the Docker copy subset used to snapshot and inject labeled files.
type ArchiveAPI interface {
	CopyFromContainer(
		ctx context.Context,
		containerID string,
		options dockerClient.CopyFromContainerOptions,
	) (dockerClient.CopyFromContainerResult, error)
	CopyToContainer(
		ctx context.Context,
		containerID string,
		options dockerClient.CopyToContainerOptions,
	) (dockerClient.CopyToContainerResult, error)
}

// copyFileEntry is one labeled file captured from a container.
type copyFileEntry struct {
	target  string
	archive []byte
}

// copyFileSnapshot holds tar archives to inject into a replacement container.
type copyFileSnapshot struct {
	files []copyFileEntry
}

// empty reports whether the snapshot contains no files.
//
// Returns:
//   - bool: True when no files were captured.
func (s copyFileSnapshot) empty() bool {
	return len(s.files) == 0
}

// size returns the total archive bytes in the snapshot.
//
// Returns:
//   - int64: Sum of stored tar archive lengths.
func (s copyFileSnapshot) size() int64 {
	var total int64

	for _, file := range s.files {
		total += int64(len(file.archive))
	}

	return total
}

// parseCopyFileLabel splits and validates comma-separated in-container paths.
//
// Parameters:
//   - value: Raw copy-file label value.
//
// Returns:
//   - []string: Clean absolute paths in label order, with duplicates removed.
//   - error: Non-nil if any path is invalid.
func parseCopyFileLabel(value string) ([]string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		filePath := strings.TrimSpace(part)
		if filePath == "" {
			continue
		}

		err := validateCopyFilePath(filePath)
		if err != nil {
			return nil, err
		}

		if _, ok := seen[filePath]; ok {
			continue
		}

		seen[filePath] = struct{}{}
		out = append(out, filePath)
	}

	return out, nil
}

// validateCopyFilePath rejects relative, root, unclean, or parent-traversal paths.
//
// Parameters:
//   - filePath: Candidate in-container path.
//
// Returns:
//   - error: Non-nil if the path is not a clean absolute file path.
func validateCopyFilePath(filePath string) error {
	if filePath == "" || filePath == "/" {
		return fmt.Errorf("%w: %q", errCopyFileInvalidPath, filePath)
	}

	if !path.IsAbs(filePath) || strings.Contains(filePath, "\\") {
		return fmt.Errorf("%w: %q", errCopyFileInvalidPath, filePath)
	}

	if path.Clean(filePath) != filePath {
		return fmt.Errorf("%w: %q", errCopyFileInvalidPath, filePath)
	}

	for elem := range strings.SplitSeq(filePath, "/") {
		if elem == ".." {
			return fmt.Errorf("%w: %q", errCopyFileInvalidPath, filePath)
		}
	}

	return nil
}

// copyFilePaths reads and validates the copy-file label on a container.
//
// Parameters:
//   - c: Container whose labels should be read.
//
// Returns:
//   - []string: Validated in-container paths, or nil when the label is absent.
//   - error: Non-nil if the label contains an invalid path.
func copyFilePaths(c types.Container) ([]string, error) {
	if c == nil {
		return nil, nil
	}

	val, ok := c.GetLabel(copyFileLabel)
	if !ok || val == "" {
		return nil, nil
	}

	return parseCopyFileLabel(val)
}

// copyFilePathIsMounted reports whether path is a mount or under a mount.
//
// Bind mounts and volumes are already preserved on recreation, so labeled
// copies of those paths are skipped.
//
// Parameters:
//   - c: Container whose inspect mounts should be checked.
//   - filePath: Absolute in-container path.
//
// Returns:
//   - bool: True if the path is covered by an existing mount.
func copyFilePathIsMounted(c types.Container, filePath string) bool {
	info := c.ContainerInfo()
	if info == nil {
		return false
	}

	for _, mount := range info.Mounts {
		if pathIsUnderMount(filePath, mount.Destination) {
			return true
		}
	}

	if info.HostConfig == nil {
		return false
	}

	for _, mount := range info.HostConfig.Mounts {
		if pathIsUnderMount(filePath, mount.Target) {
			return true
		}
	}

	for _, bind := range info.HostConfig.Binds {
		if dest := bindDestination(bind); dest != "" && pathIsUnderMount(filePath, dest) {
			return true
		}
	}

	return false
}

// pathIsUnderMount reports whether filePath is mountDest or a descendant.
//
// Parameters:
//   - filePath: Absolute in-container file path.
//   - mountDest: Absolute mount destination.
//
// Returns:
//   - bool: True if filePath is at or under mountDest.
func pathIsUnderMount(filePath, mountDest string) bool {
	filePath = path.Clean(filePath)
	mountDest = path.Clean(mountDest)

	if mountDest == "" || mountDest == "." {
		return false
	}

	if filePath == mountDest {
		return true
	}

	return strings.HasPrefix(filePath, mountDest+"/")
}

// bindDestination returns the container path from a Unix bind string.
//
// Parameters:
//   - bind: HostConfig.Binds entry in source:destination[:options] form.
//
// Returns:
//   - string: Container destination, or empty if the bind is malformed.
func bindDestination(bind string) string {
	parts := strings.Split(bind, ":")
	if len(parts) < bindStringMinParts {
		return ""
	}

	return parts[1]
}

// snapshotCopyFiles reads labeled files from the source container into memory.
//
// Missing paths are skipped. Invalid labels, directories, oversized files, and
// unexpected copy errors fail the snapshot so the source container is not removed.
//
// Parameters:
//   - log: Logger for debug messages.
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker copy API.
//   - source: Source container to copy from.
//
// Returns:
//   - copyFileSnapshot: Captured tar archives keyed by in-container path.
//   - error: Non-nil if a labeled path cannot be snapshotted.
func snapshotCopyFiles(
	log *zerolog.Logger,
	ctx context.Context,
	api ArchiveAPI,
	source types.Container,
) (copyFileSnapshot, error) {
	paths, err := copyFilePaths(source)
	if err != nil {
		return copyFileSnapshot{}, err
	}

	if len(paths) == 0 {
		return copyFileSnapshot{}, nil
	}

	clogVal := log.With().
		Str("container", source.Name()).
		Str("id", source.ID().ShortID()).
		Logger()
	clog := &clogVal
	files := make([]copyFileEntry, 0, len(paths))

	var total int64

	for _, filePath := range paths {
		if copyFilePathIsMounted(source, filePath) {
			clog.Debug().
				Str("path", filePath).
				Msg("Skipping copy-file path because it is a mount")

			continue
		}

		entry, skip, snapErr := snapshotCopyFile(ctx, clog, api, string(source.ID()), filePath)
		if snapErr != nil {
			return copyFileSnapshot{}, snapErr
		}

		if skip {
			continue
		}

		next := total + int64(len(entry.archive))
		if next > maxCopyFileStoreBytes {
			return copyFileSnapshot{}, fmt.Errorf("%w: %d bytes", errCopyFileStoreFull, next)
		}

		files = append(files, entry)
		total = next
	}

	return copyFileSnapshot{files: files}, nil
}

// snapshotCopyFile copies one labeled path from a container into a tar archive.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - clog: Logger with container fields.
//   - api: Docker copy API.
//   - containerID: Source container ID.
//   - filePath: Absolute in-container path.
//
// Returns:
//   - copyFileEntry: Captured archive when the path exists and is a file.
//   - bool: True when the path should be skipped.
//   - error: Non-nil if the copy fails for a reason other than a missing path.
func snapshotCopyFile(
	ctx context.Context,
	clog *zerolog.Logger,
	api ArchiveAPI,
	containerID string,
	filePath string,
) (copyFileEntry, bool, error) {
	result, err := api.CopyFromContainer(ctx, containerID, dockerClient.CopyFromContainerOptions{
		SourcePath: filePath,
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.Debug().
				Str("path", filePath).
				Msg("Skipping missing copy-file path")

			return copyFileEntry{}, true, nil
		}

		return copyFileEntry{}, false, fmt.Errorf("%w: %s: %w", errCopyFileSnapshotFailed, filePath, err)
	}

	if result.Content != nil {
		defer result.Content.Close()
	}

	if result.Stat.Mode.IsDir() {
		return copyFileEntry{}, false, fmt.Errorf("%w: %q", errCopyFileDirectory, filePath)
	}

	if result.Stat.LinkTarget != "" || result.Stat.Mode.Type() == os.ModeSymlink {
		return copyFileEntry{}, false, fmt.Errorf("%w: %q", errCopyFileSymlink, filePath)
	}

	if result.Stat.Size > maxCopyFileBytes {
		return copyFileEntry{}, false, fmt.Errorf("%w: %q", errCopyFileTooLarge, filePath)
	}

	if result.Content == nil {
		return copyFileEntry{}, false, fmt.Errorf("%w: %s: empty archive stream", errCopyFileSnapshotFailed, filePath)
	}

	limited := io.LimitReader(result.Content, maxCopyFileTarBytes+1)

	archive, err := io.ReadAll(limited)
	if err != nil {
		return copyFileEntry{}, false, fmt.Errorf("%w: %s: %w", errCopyFileSnapshotFailed, filePath, err)
	}

	if int64(len(archive)) > maxCopyFileTarBytes {
		return copyFileEntry{}, false, fmt.Errorf("%w: %q", errCopyFileTooLarge, filePath)
	}

	sanitized, err := sanitizeCopyFileArchive(archive, filePath)
	if err != nil {
		return copyFileEntry{}, false, err
	}

	return copyFileEntry{target: filePath, archive: sanitized}, false, nil
}

// sanitizeCopyFileArchive rewrites a Docker copy archive to a single regular file.
//
// The output tar has the destination basename, permission bits without setuid,
// setgid, or sticky, and the original uid, gid, and file contents.
//
// Parameters:
//   - raw: Tar stream returned by CopyFromContainer.
//   - destPath: Absolute in-container destination path.
//
// Returns:
//   - []byte: Sanitized tar archive.
//   - error: Non-nil if the archive is not a single regular file at destPath.
func sanitizeCopyFileArchive(raw []byte, destPath string) ([]byte, error) {
	base := path.Base(destPath)
	reader := tar.NewReader(bytes.NewReader(raw))

	var (
		body    []byte
		header  *tar.Header
		found   bool
		readErr error
	)

	for {
		next, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("%w: %w", errCopyFileUnsafeArchive, err)
		}

		switch next.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		case tar.TypeReg:
			if found {
				return nil, fmt.Errorf("%w: extra member %q", errCopyFileUnsafeArchive, next.Name)
			}

			if !copyFileTarNameOK(next.Name, base) {
				return nil, fmt.Errorf("%w: %q", errCopyFileUnsafeArchive, next.Name)
			}

			if next.Size > maxCopyFileBytes {
				return nil, fmt.Errorf("%w: %q", errCopyFileTooLarge, destPath)
			}

			body, readErr = io.ReadAll(io.LimitReader(reader, maxCopyFileBytes+1))
			if readErr != nil {
				return nil, fmt.Errorf("%w: %s: %w", errCopyFileSnapshotFailed, destPath, readErr)
			}

			if int64(len(body)) > maxCopyFileBytes {
				return nil, fmt.Errorf("%w: %q", errCopyFileTooLarge, destPath)
			}

			header = next
			found = true
		case tar.TypeSymlink, tar.TypeLink:
			return nil, fmt.Errorf("%w: %q", errCopyFileSymlink, destPath)
		default:
			return nil, fmt.Errorf("%w: %q", errCopyFileUnsafeArchive, next.Name)
		}
	}

	if !found {
		return nil, fmt.Errorf("%w: empty archive", errCopyFileUnsafeArchive)
	}

	return writeSanitizedCopyFileTar(base, body, header)
}

// copyFileTarNameOK reports whether a tar member name is the destination basename.
//
// Parameters:
//   - name: Tar header name.
//   - base: Expected file basename.
//
// Returns:
//   - bool: True when name has no parent traversal and its base name matches.
func copyFileTarNameOK(name, base string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}

	return path.Base(path.Clean(name)) == base
}

// writeSanitizedCopyFileTar builds a one-member regular-file tar.
//
// Parameters:
//   - base: File basename to write into the tar header.
//   - body: File contents.
//   - src: Original tar header for uid, gid, mode, and modtime.
//
// Returns:
//   - []byte: Tar archive.
//   - error: Non-nil if the tar cannot be written.
func writeSanitizedCopyFileTar(base string, body []byte, src *tar.Header) ([]byte, error) {
	var buf bytes.Buffer

	writer := tar.NewWriter(&buf)

	err := writer.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     base,
		Size:     int64(len(body)),
		Mode:     src.Mode & unixFilePermMask,
		Uid:      src.Uid,
		Gid:      src.Gid,
		Uname:    src.Uname,
		Gname:    src.Gname,
		ModTime:  src.ModTime,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCopyFileUnsafeArchive, err)
	}

	_, err = writer.Write(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCopyFileUnsafeArchive, err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCopyFileUnsafeArchive, err)
	}

	return buf.Bytes(), nil
}

// injectCopyFiles writes a snapshot into a newly created container before start.
//
// Parameters:
//   - log: Logger for debug messages.
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker copy API.
//   - containerID: Replacement container ID.
//   - source: Source container whose host config is checked for a read-only root.
//   - snapshot: Files captured from the source container.
//
// Returns:
//   - error: Non-nil if a file cannot be written into the new container.
func injectCopyFiles(
	log *zerolog.Logger,
	ctx context.Context,
	api ArchiveAPI,
	containerID string,
	source types.Container,
	snapshot copyFileSnapshot,
) error {
	if snapshot.empty() {
		return nil
	}

	if containerHasReadOnlyRoot(source) {
		log.Debug().
			Str("container", source.Name()).
			Msg("Skipping copy-file inject because the root filesystem is read-only")

		return nil
	}

	clogVal := log.With().
		Str("container", source.Name()).
		Str("new_id", containerID).
		Logger()
	clog := &clogVal

	for _, file := range snapshot.files {
		dest := path.Dir(file.target)

		_, err := api.CopyToContainer(ctx, containerID, dockerClient.CopyToContainerOptions{
			DestinationPath: dest,
			Content:         bytes.NewReader(file.archive),
			CopyUIDGID:      true,
		})
		if err != nil {
			return fmt.Errorf("%w: %s: %w", errCopyFileInjectFailed, file.target, err)
		}

		clog.Debug().
			Str("path", file.target).
			Msg("Copied file into new container")
	}

	return nil
}

// containerHasReadOnlyRoot reports whether the container root filesystem is read-only.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - bool: True when HostConfig.ReadonlyRootfs is set.
func containerHasReadOnlyRoot(c types.Container) bool {
	info := c.ContainerInfo()
	if info == nil || info.HostConfig == nil {
		return false
	}

	return info.HostConfig.ReadonlyRootfs
}

// copyArchiveAPI returns the Docker copy API used for snapshot and inject.
//
// Returns:
//   - ArchiveAPI: Test override when set, otherwise the Docker client.
func (c *client) copyArchiveAPI() ArchiveAPI {
	if c.copyAPI != nil {
		return c.copyAPI
	}

	return c.api
}

// SnapshotCopyFiles captures labeled files before the source is removed.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - source: Source container about to be stopped and removed.
//
// Returns:
//   - error: Non-nil if a labeled path cannot be snapshotted.
func (c *client) SnapshotCopyFiles(ctx context.Context, source types.Container) error {
	snapshot, err := snapshotCopyFiles(c.logger(), ctx, c.copyArchiveAPI(), source)
	if err != nil {
		return err
	}

	if snapshot.empty() {
		return nil
	}

	return c.storeCopyFileSnapshot(source.ID(), snapshot)
}

// storeCopyFileSnapshot records a snapshot if it fits in the in-flight byte cap.
//
// Parameters:
//   - containerID: Source container ID.
//   - snapshot: Sanitized archives to hold until inject.
//
// Returns:
//   - error: Non-nil if storing the snapshot would exceed maxCopyFileStoreBytes.
func (c *client) storeCopyFileSnapshot(
	containerID types.ContainerID,
	snapshot copyFileSnapshot,
) error {
	c.copyFileMu.Lock()
	defer c.copyFileMu.Unlock()

	if c.copyFiles == nil {
		c.copyFiles = make(map[types.ContainerID]copyFileSnapshot)
	}

	existing, ok := c.copyFiles[containerID]
	if ok {
		c.copyFileBytes -= existing.size()
	}

	if c.copyFileBytes < 0 {
		c.copyFileBytes = 0
	}

	next := c.copyFileBytes + snapshot.size()
	if next > maxCopyFileStoreBytes {
		if ok {
			c.copyFileBytes += existing.size()
		}

		return fmt.Errorf("%w: %d bytes", errCopyFileStoreFull, next)
	}

	c.copyFiles[containerID] = snapshot
	c.copyFileBytes = next

	return nil
}

// injectStoredCopyFiles writes a stored snapshot into a newly created container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - source: Source container whose snapshot should be injected.
//   - newID: ID of the replacement container.
//
// Returns:
//   - error: Non-nil if a stored file cannot be written into the new container.
func (c *client) injectStoredCopyFiles(
	ctx context.Context,
	source types.Container,
	newID types.ContainerID,
) error {
	c.copyFileMu.Lock()
	snapshot, ok := c.dropCopyFileLocked(source.ID())
	c.copyFileMu.Unlock()

	if !ok {
		return nil
	}

	return injectCopyFiles(c.logger(), ctx, c.copyArchiveAPI(), string(newID), source, snapshot)
}

// DiscardCopyFiles drops a leftover snapshot for the source container.
//
// Parameters:
//   - containerID: Source container ID whose snapshot should be discarded.
func (c *client) DiscardCopyFiles(containerID types.ContainerID) {
	c.copyFileMu.Lock()
	defer c.copyFileMu.Unlock()

	c.dropCopyFileLocked(containerID)
}

// dropCopyFileLocked removes a stored snapshot and subtracts its size.
//
// Parameters:
//   - containerID: Source container ID whose snapshot should be dropped.
//
// Returns:
//   - copyFileSnapshot: The removed snapshot when present.
//   - bool: True when a snapshot was stored for containerID.
func (c *client) dropCopyFileLocked(
	containerID types.ContainerID,
) (copyFileSnapshot, bool) {
	snapshot, ok := c.copyFiles[containerID]
	if !ok {
		return copyFileSnapshot{}, false
	}

	delete(c.copyFiles, containerID)

	c.copyFileBytes -= snapshot.size()
	if c.copyFileBytes < 0 {
		c.copyFileBytes = 0
	}

	return snapshot, true
}
