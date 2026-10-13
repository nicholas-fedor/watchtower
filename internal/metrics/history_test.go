package metrics

import "testing"

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

func TestMetrics_RecordCheckWrapsRingBuffer(t *testing.T) {
	m := &Metrics{}

	for range historyBufferSize + 3 {
		m.RecordCheck(1, 0, 0)
	}

	if got := len(m.GetHistory(nil, nil, 0)); got != historyBufferSize {
		t.Errorf("history holds %d entries, want %d", got, historyBufferSize)
	}
}
