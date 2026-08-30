package cmd

import "errors"

var (
	// errRunRequired means --run was omitted.
	errRunRequired = errors.New("flag --run is required")
	// errLogsRequired means --run or --case was omitted.
	errLogsRequired = errors.New("flags --run and --case are required")
	// errServeUnreachable means no serve process is listening.
	errServeUnreachable = errors.New("control plane not reachable")
	// errRunCasesFailed means the sitting finished with failed cases.
	errRunCasesFailed = errors.New("e2e cases failed")
	// errRunHarness means the sitting itself failed to execute.
	errRunHarness = errors.New("run failed")
	// errRunStopped means the sitting was canceled or interrupted.
	errRunStopped = errors.New("run stopped")
)
