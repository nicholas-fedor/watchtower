package docker

const (
	// permDir is the mode for created directories.
	permDir = 0o750
	// permFile is the mode for written files.
	permFile = 0o600
	// permTarFile is the tar mode for Dockerfiles in build context.
	permTarFile = 0o644
	// permTarExec is the tar mode for binaries in build context.
	permTarExec = 0o755
)
