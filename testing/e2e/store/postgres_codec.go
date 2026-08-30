package store

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"
)

// rowScanner is pgx.Row or pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRun reads one runs table row.
//
// Parameters:
//   - row: Query row or rows iterator.
//
// Returns:
//   - Run: Sitting.
//   - error: Scan failure.
func scanRun(row rowScanner) (Run, error) {
	var (
		run                     Run
		status                  string
		startedAt, finishedAt   *time.Time
		generator, topic        string
		filter, filePath, shard string
		seed                    int64
		offsetN, limitN         int
		workers                 int
		keep                    bool
	)

	err := row.Scan(
		&run.ID, &run.Label, &run.CreatedAt, &startedAt, &finishedAt, &status,
		&generator, &seed, &topic, &filter, &filePath, &shard,
		&offsetN, &limitN, &workers, &keep, &run.Passed, &run.Failed, &run.Skipped, &run.Error,
	)
	if err != nil {
		return Run{}, err
	}

	run.Status = RunStatus(status)
	if startedAt != nil {
		run.StartedAt = *startedAt
	}

	if finishedAt != nil {
		run.FinishedAt = *finishedAt
	}

	run.Spec = Spec{
		Generator: generator,
		Seed:      seed,
		Topic:     topic,
		Filter:    filter,
		FilePath:  filePath,
		Shard:     shard,
		Offset:    offsetN,
		Limit:     limitN,
		Workers:   workers,
		Keep:      keep,
	}

	return run, nil
}

// scanCase reads one cases table row.
//
// Parameters:
//   - row: Query row or rows iterator.
//
// Returns:
//   - Case: Case row.
//   - error: Scan failure.
func scanCase(row rowScanner) (Case, error) {
	var (
		item                             Case
		status                           string
		factors, argv, env               []byte
		expect, before, after, porcelain []byte
		startedAt, finishedAt            *time.Time
	)

	err := row.Scan(
		&item.RunID, &item.CaseID, &status, &factors, &expect, &argv, &env,
		&item.Error, &item.DurationMs, &before, &after, &porcelain, &item.HTTPDetails,
		&startedAt, &finishedAt,
	)
	if err != nil {
		return Case{}, err
	}

	item.Status = CaseStatus(status)

	var decodeErr error

	item.Factors, decodeErr = unmarshalMap(factors)
	if decodeErr != nil {
		return Case{}, fmt.Errorf("factors json: %w", decodeErr)
	}

	item.Env, decodeErr = unmarshalMap(env)
	if decodeErr != nil {
		return Case{}, fmt.Errorf("env json: %w", decodeErr)
	}

	item.Argv, decodeErr = unmarshalStrings(argv)
	if decodeErr != nil {
		return Case{}, fmt.Errorf("argv json: %w", decodeErr)
	}

	item.Expect = expect
	item.InspectBefore = before
	item.InspectAfter = after
	item.Porcelain = porcelain

	if startedAt != nil {
		item.StartedAt = *startedAt
	}

	if finishedAt != nil {
		item.FinishedAt = *finishedAt
	}

	return item, nil
}

// nullTime returns nil for the zero time so Postgres TIMESTAMPTZ stays NULL.
//
// Parameters:
//   - t: Timestamp.
//
// Returns:
//   - any: t, or nil when zero.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}

	return t
}

// nullJSON returns nil for empty raw JSON so JSONB columns stay NULL.
//
// Parameters:
//   - raw: JSON document.
//
// Returns:
//   - any: Bytes, or nil when empty.
func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}

	return []byte(raw)
}

// jsonMap marshals a string map, using {} for nil.
//
// Parameters:
//   - v: Map to encode.
//
// Returns:
//   - []byte: JSON object.
func jsonMap(v map[string]string) []byte {
	if v == nil {
		return []byte(`{}`)
	}

	raw, err := jsonv2.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}

	return raw
}

// jsonSlice marshals a string slice, using [] for nil.
//
// Parameters:
//   - v: Slice to encode.
//
// Returns:
//   - []byte: JSON array.
func jsonSlice(v []string) []byte {
	if v == nil {
		return []byte(`[]`)
	}

	raw, err := jsonv2.Marshal(v)
	if err != nil {
		return []byte(`[]`)
	}

	return raw
}

// unmarshalMap decodes a JSON object into a string map.
//
// Parameters:
//   - raw: JSON object bytes.
//
// Returns:
//   - map[string]string: Decoded map, or nil when raw is empty.
func unmarshalMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var out map[string]string

	err := jsonv2.Unmarshal(raw, &out)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// unmarshalStrings decodes a JSON array into strings.
//
// Parameters:
//   - raw: JSON array bytes.
//
// Returns:
//   - []string: Decoded slice, or nil when raw is empty.
func unmarshalStrings(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var out []string

	err := jsonv2.Unmarshal(raw, &out)
	if err != nil {
		return nil, err
	}

	return out, nil
}
