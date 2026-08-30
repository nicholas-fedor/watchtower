package api

import "github.com/nicholas-fedor/watchtower/testing/e2e/store"

// Spec is the operator input for POST /v1/runs.
type Spec = store.Spec

// Run is one sitting snapshot from the JSON API.
type Run = store.Run

const (
	// RunQueued is waiting for the execution slot.
	RunQueued = store.RunQueued
	// RunRunning has workers executing cases.
	RunRunning = store.RunRunning
	// RunInterrupted stopped with work remaining.
	RunInterrupted = store.RunInterrupted
	// RunCompleted finished the selected stream. Case pass/fail is in the counts.
	RunCompleted = store.RunCompleted
	// RunFailed means the sitting itself did not finish.
	RunFailed = store.RunFailed
	// RunCanceled was stopped by the operator.
	RunCanceled = store.RunCanceled
)
