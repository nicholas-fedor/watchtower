package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
)

// ProjectRef is a Compose project Watchtower can apply.
type ProjectRef struct {
	Name        string
	Dir         string
	ConfigFiles []string
}

// ResolveProjectDir returns the Compose project the user opted into for this container.
//
// Local Compose apply is explicit:
//  1. com.centurylinklabs.watchtower.compose-dir
//  2. --compose-project / WATCHTOWER_COMPOSE_PROJECT keyed by Compose project name
//
// Compose's own working_dir label is not used. An explicit path that is missing
// or has no compose file is an error. Watchtower does not fall back to a Git URL context.
//
// Parameters:
//   - log: Process logger.
//   - labels: Container labels.
//   - projectDirs: Map of Compose project name to a path inside Watchtower.
//
// Returns:
//   - ProjectRef: Directory when the user opted in. Dir is empty when they did not.
//   - error: Non-nil when an opted-in path is not a readable Compose project.
func ResolveProjectDir(log *zerolog.Logger, labels, projectDirs map[string]string) (ProjectRef, error) {
	name := GetProjectName(log, labels)

	if dir := labelValue(labels, WatchtowerComposeDirLabel); dir != "" {
		return projectRefOrErr(name, dir, labels)
	}

	if name != "" {
		if dir := strings.TrimSpace(projectDirs[name]); dir != "" {
			return projectRefOrErr(name, dir, labels)
		}
	}

	return ProjectRef{}, nil
}

// projectRefOrErr returns a project ref when dir is a readable Compose project.
//
// Parameters:
//   - name: Compose project name.
//   - dir: Path inside Watchtower.
//   - labels: Container labels for optional compose file list.
//
// Returns:
//   - ProjectRef: Directory and optional compose files.
//   - error: ErrProjectDir when dir is missing or has no compose file.
func projectRefOrErr(name, dir string, labels map[string]string) (ProjectRef, error) {
	if !readableProjectDir(dir) {
		return ProjectRef{}, fmt.Errorf("%w: %s", ErrProjectDir, dir)
	}

	return ProjectRef{
		Name:        name,
		Dir:         dir,
		ConfigFiles: configFiles(labels, dir),
	}, nil
}

// labelValue returns a trimmed label, or empty when labels is nil.
//
// Parameters:
//   - labels: Container labels.
//   - key: Label key.
//
// Returns:
//   - string: Label value, or empty.
func labelValue(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}

	return strings.TrimSpace(labels[key])
}

// configFiles splits Compose's config_files label into absolute paths.
//
// Parameters:
//   - labels: Container labels.
//   - dir: Project directory used to resolve relative paths.
//
// Returns:
//   - []string: Compose file paths, or nil when the label is absent.
func configFiles(labels map[string]string, dir string) []string {
	raw := labelValue(labels, ComposeConfigFilesLabel)
	if raw == "" {
		return nil
	}

	var out []string

	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if !filepath.IsAbs(part) {
			part = filepath.Join(dir, part)
		}

		out = append(out, part)
	}

	return out
}

// readableProjectDir reports whether dir exists and contains a compose file.
//
// Parameters:
//   - dir: Candidate project directory.
//
// Returns:
//   - bool: True when a compose.yaml or docker-compose.yaml is present.
func readableProjectDir(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if fileExists(filepath.Join(dir, name)) {
			return true
		}
	}

	return false
}

// fileExists reports whether path is a regular file.
//
// Parameters:
//   - path: Filesystem path.
//
// Returns:
//   - bool: True when the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}
