package assert

import "errors"

var (
	// ErrPorcelainMissingName means a porcelain row omitted name.
	ErrPorcelainMissingName = errors.New("porcelain container missing name")
	// ErrPorcelainMissingImage means a porcelain row omitted image.
	ErrPorcelainMissingImage = errors.New("porcelain container missing image")
	// ErrPorcelainNoUpdate means no container reported an update.
	ErrPorcelainNoUpdate = errors.New("porcelain expected an updated container")
	// ErrSecretLeaked means a secret substring appeared in captured output.
	ErrSecretLeaked = errors.New("secret value leaked into captured output")
	// ErrConfigEchoedAuth means /v1/config echoed an authorization header.
	ErrConfigEchoedAuth = errors.New("v1/config echoed an authorization header")
	// ErrDependencyOrder means stop/start order did not match depends-on.
	ErrDependencyOrder = errors.New("depends-on order")
	// ErrUnexpectedUpdate means an image ID changed when no update was expected.
	ErrUnexpectedUpdate = errors.New("image id changed but no update was expected")
	// ErrMissingStop means a dependent was not stopped.
	ErrMissingStop = errors.New("expected container was not stopped")
)
