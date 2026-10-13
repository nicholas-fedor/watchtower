package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetrics_RecordCheck(t *testing.T) {
	m := &Metrics{}

	m.RecordHistory(&Metric{Scanned: 5, Updated: 2, Failed: 1})
	m.RecordCheck(21, 3, 1)

	history := m.GetHistory(nil, nil, 0)
	if len(history) != 2 {
		t.Fatalf("GetHistory() returned %d entries, want 2", len(history))
	}

	scan, check := history[0], history[1]

	if scan.Kind != HistoryKindScan {
		t.Errorf("scan entry kind = %q, want %q", scan.Kind, HistoryKindScan)
	}

	if scan.UpdatesAvailable != 0 {
		t.Errorf("scan entry updates available = %d, want 0", scan.UpdatesAvailable)
	}

	if check.Kind != HistoryKindCheck {
		t.Errorf("check entry kind = %q, want %q", check.Kind, HistoryKindCheck)
	}

	if check.Scanned != 21 || check.UpdatesAvailable != 3 || check.Failed != 1 {
		t.Errorf("check entry = %+v, want 21 checked, 3 available, 1 failed", check)
	}

	// A check changes nothing, so it must not look like an update.
	if check.Updated != 0 || check.Restarted != 0 {
		t.Errorf("check entry reports changes: %+v", check)
	}
}

func TestMetrics_PersistHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	first := &Metrics{}
	first.PersistHistory(nil, path)
	first.RecordHistory(&Metric{Scanned: 5, Updated: 2})
	first.RecordCheck(21, 3, 0)

	// A new process over the same file sees what the first one recorded.
	second := &Metrics{}
	second.PersistHistory(nil, path)

	history := second.GetHistory(nil, nil, 0)
	if len(history) != 2 {
		t.Fatalf("restored %d entries, want 2", len(history))
	}

	if history[0].Kind != HistoryKindScan || history[0].Updated != 2 {
		t.Errorf("first restored entry = %+v, want the scan", history[0])
	}

	if history[1].Kind != HistoryKindCheck || history[1].UpdatesAvailable != 3 {
		t.Errorf("second restored entry = %+v, want the check", history[1])
	}

	// New entries are appended to the restored ones and saved again.
	second.RecordCheck(21, 0, 0)

	third := &Metrics{}
	third.PersistHistory(nil, path)

	if got := len(third.GetHistory(nil, nil, 0)); got != 3 {
		t.Errorf("restored %d entries after a further check, want 3", got)
	}
}

func TestMetrics_PersistHistoryIgnoresUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	err := os.WriteFile(path, []byte("{not json"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	m := &Metrics{}
	m.PersistHistory(nil, path)

	if got := len(m.GetHistory(nil, nil, 0)); got != 0 {
		t.Fatalf("history holds %d entries, want 0", got)
	}

	// The store keeps working and replaces the bad file on the next entry.
	m.RecordCheck(4, 1, 0)

	restored := &Metrics{}
	restored.PersistHistory(nil, path)

	if got := len(restored.GetHistory(nil, nil, 0)); got != 1 {
		t.Errorf("restored %d entries, want 1", got)
	}
}

func TestMetrics_PersistHistoryKeepsBufferBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	m := &Metrics{}
	m.PersistHistory(nil, path)

	for range historyBufferSize + 5 {
		m.RecordCheck(1, 0, 0)
	}

	restored := &Metrics{}
	restored.PersistHistory(nil, path)

	if got := len(restored.GetHistory(nil, nil, 0)); got != historyBufferSize {
		t.Errorf("restored %d entries, want %d", got, historyBufferSize)
	}
}

func TestMetrics_RecordCheckWrapsRingBuffer(t *testing.T) {
	m := &Metrics{}

	for range historyBufferSize + 3 {
		m.RecordCheck(1, 0, 0)
	}

	if got := len(m.GetHistory(nil, nil, 0)); got != historyBufferSize {
		t.Errorf("history holds %d entries, want %d", got, historyBufferSize)
	}
}
