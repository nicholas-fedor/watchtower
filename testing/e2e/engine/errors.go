package engine

import "errors"

const (
	// permDir is the mode for checkpoint directories.
	permDir = 0o750
	// permFile is the mode for checkpoint JSON files.
	permFile = 0o600
)

var (
	// ErrNoRunFunc is returned when Scheduler.Run is nil.
	ErrNoRunFunc = errors.New("scheduler run func is nil")
	// ErrFileGeneratorNeedsPath means generator file was selected without a path.
	ErrFileGeneratorNeedsPath = errors.New("generator file requires --file")
	// ErrShardSyntax means --shard was not i/n.
	ErrShardSyntax = errors.New("shard must be i/n")
	// ErrShardRange means i/n is out of range.
	ErrShardRange = errors.New("shard out of range")
	// ErrFlagsUncovered means Model() does not cover every RegisterAll flag.
	ErrFlagsUncovered = errors.New("doctor: flags missing from Model")
	// ErrUnknownTopic means --topic did not match a named development slice.
	ErrUnknownTopic = errors.New("unknown topic")
)
