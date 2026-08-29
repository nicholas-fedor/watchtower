package run

import "errors"

var (
	// ErrImageIDUnchanged means Expect updated but inspect image id did not change.
	ErrImageIDUnchanged = errors.New("expected image id to change on update")
	// ErrSubjectMissing means the subject container was gone after the session.
	ErrSubjectMissing = errors.New("container not found after session")
	// ErrCasesFailed means at least one executed case failed.
	ErrCasesFailed = errors.New("e2e cases failed")
	// ErrReplayNeedsCase means replay was invoked without a case directory.
	ErrReplayNeedsCase = errors.New("replay requires --case")
	// ErrReplayUseFile means meta.json replay is not YAML. Use --generator file.
	ErrReplayUseFile = errors.New("replay YAML cases with e2e run --generator file --file")
	// errExtraEnvMissing means Topology.ExtraEnv was dropped on recreate.
	errExtraEnvMissing = errors.New("fidelity: extra env missing after recreate")
	// errNoRateLimitLog means a rate-limited case produced no pull or quota evidence.
	errNoRateLimitLog = errors.New("expected rate-limit log line")
	// errPreservedMAC means a 02:42 engine MAC was copied onto the new container.
	errPreservedMAC = errors.New("engine-generated MAC was preserved on recreate")
	// errStaleImageLeft means --cleanup did not remove the previous image ID.
	errStaleImageLeft = errors.New("cleanup left the previous image")
	// errRejectConfigExitZero means reject-config was expected but Watchtower exited 0.
	errRejectConfigExitZero = errors.New("expected reject-config but watchtower exited 0")
)
