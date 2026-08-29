package docker

const (
	// permTarFile is the tar mode for Dockerfiles in build context.
	permTarFile = 0o644
	// permTarExec is the tar mode for binaries in build context.
	permTarExec = 0o755
)
