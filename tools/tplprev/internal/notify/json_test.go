package notify

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/tplprev/internal/report"
)

type stubReport struct {
	scanned []report.ContainerReport
	updated []report.ContainerReport
	failed  []report.ContainerReport
}

type stubContainerError struct {
	id    report.ContainerID
	name  string
	oldID report.ImageID
	newID report.ImageID
	image string
	state string
	err   string
}

func (r stubReport) Scanned() []report.ContainerReport   { return r.scanned }
func (r stubReport) Updated() []report.ContainerReport   { return r.updated }
func (r stubReport) Failed() []report.ContainerReport    { return r.failed }
func (r stubReport) Skipped() []report.ContainerReport   { return nil }
func (r stubReport) Stale() []report.ContainerReport     { return nil }
func (r stubReport) Fresh() []report.ContainerReport     { return nil }
func (r stubReport) Restarted() []report.ContainerReport { return nil }
func (r stubReport) All() []report.ContainerReport       { return r.scanned }

func (c stubContainerError) ID() report.ContainerID             { return c.id }
func (c stubContainerError) Name() string                       { return c.name }
func (c stubContainerError) CurrentImageID() report.ImageID     { return c.oldID }
func (c stubContainerError) LatestImageID() report.ImageID      { return c.newID }
func (c stubContainerError) ImageName() string                  { return c.image }
func (c stubContainerError) Error() string                      { return c.err }
func (c stubContainerError) State() string                      { return c.state }
func (c stubContainerError) IsMonitorOnly() bool                { return false }
func (c stubContainerError) NewContainerID() report.ContainerID { return "" }

func TestDataMarshalJSON(t *testing.T) {
	t.Parallel()

	updated := stubContainerError{
		id:    "sha256:c79110000000aaaa",
		name:  "updt1",
		oldID: "sha256:01d110000000aaaa",
		newID: "sha256:d0a110000000aaaa",
		image: "mock/updt1:latest",
		state: "updated",
	}
	failed := stubContainerError{
		id:    "sha256:c79210000000aaaa",
		name:  "fail1",
		oldID: "sha256:01d210000000aaaa",
		newID: "sha256:d0a210000000aaaa",
		image: "mock/fail1:latest",
		state: "failed",
		err:   "execution failed",
	}

	payload := Data{
		StaticData: StaticData{Title: "Title", Host: "Host"},
		Entries: []*Entry{
			{
				Message: "Found new image",
				Data:    map[string]any{"image": "mock/updt1:latest"},
				Time:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
				Level:   "info",
			},
		},
		Report: stubReport{
			scanned: []report.ContainerReport{updated, failed},
			updated: []report.ContainerReport{updated},
			failed:  []report.ContainerReport{failed},
		},
	}

	bytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(bytes, &decoded))

	assert.Equal(t, "Title", decoded["title"])
	assert.Equal(t, "Host", decoded["host"])

	report, ok := decoded["report"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, report["scanned"], 2)
	assert.Len(t, report["updated"], 1)
	assert.Len(t, report["failed"], 1)

	failedJSON, ok := report["failed"].([]any)
	require.True(t, ok)
	failedEntry, ok := failedJSON[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "execution failed", failedEntry["error"])
	assert.Equal(t, "c79210000000", failedEntry["id"])

	entries, ok := decoded["entries"].([]any)
	require.True(t, ok)
	require.Len(t, entries, 1)
}

func TestDataMarshalJSONNilReport(t *testing.T) {
	t.Parallel()

	payload := Data{
		StaticData: StaticData{Title: "Title", Host: "Host"},
		Entries:    []*Entry{},
	}

	bytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(bytes, &decoded))
	assert.Nil(t, decoded["report"])
}
