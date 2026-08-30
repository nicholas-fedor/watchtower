package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/nicholas-fedor/watchtower/testing/e2e/host"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// indexModel is the dashboard home page.
type indexModel struct {
	// Host is the latest resource snapshot.
	Host host.Snapshot
	// HostOK is false when discovery failed.
	HostOK bool
	// Runs are sittings newest first.
	Runs []store.Run
	// Err is a store load failure.
	Err string
}

// runModel is one sitting's live page.
type runModel struct {
	// Run is the sitting snapshot.
	Run store.Run
	// Cases are the current page of cases.
	Cases []store.Case
	// Total is the unpaginated match count.
	Total int
	// Status is the case-status query, empty meaning all.
	Status string
	// Err is a load failure.
	Err string
}

// statusFilter is one status chip on the sitting page.
type statusFilter struct {
	// Query is the status query string. Empty means all.
	Query string
	// Label is the dashboard link text.
	Label string
}

// statusFilters returns sitting-page status links.
//
// Returns:
//   - []statusFilter: all/fail/pass/running/skip.
func statusFilters() []statusFilter {
	return []statusFilter{
		{Query: "", Label: "all"},
		{Query: "fail", Label: "fail"},
		{Query: "pass", Label: "pass"},
		{Query: "running", Label: "running"},
		{Query: "skip", Label: "skip"},
	}
}

const (
	// shortIDLen is how many UUID characters the dashboard shows.
	shortIDLen = 8
	// gibBytes converts host memory to GiB.
	gibBytes = 1 << 30
)

// shortID truncates a UUID for table display.
//
// Parameters:
//   - id: Full run UUID.
//
// Returns:
//   - string: First shortIDLen characters.
func shortID(id string) string {
	if len(id) > shortIDLen {
		return id[:shortIDLen]
	}

	return id
}

// fmtInt formats n in decimal.
//
// Parameters:
//   - n: Integer.
//
// Returns:
//   - string: Decimal text.
func fmtInt(n int) string {
	return strconv.Itoa(n)
}

// fmtInt64 formats n in decimal.
//
// Parameters:
//   - n: Integer.
//
// Returns:
//   - string: Decimal text.
func fmtInt64(n int64) string {
	return strconv.FormatInt(n, 10)
}

// fmtGiB formats a byte count as GiB with one decimal.
//
// Parameters:
//   - bytes: Size in bytes.
//
// Returns:
//   - string: GiB text.
func fmtGiB(bytes uint64) string {
	return fmt.Sprintf("%.1f", float64(bytes)/float64(gibBytes))
}

// joinIDs joins case IDs with a comma for the live-status chip.
//
// Parameters:
//   - ids: Case identifiers.
//
// Returns:
//   - string: Comma-separated list.
func joinIDs(ids []string) string {
	return strings.Join(ids, ", ")
}

// shouldPoll reports whether the sitting fragment should keep polling.
//
// Returns:
//   - bool: True when a sitting id is present.
func (m runModel) shouldPoll() bool {
	return m.Run.ID != ""
}

// pagePath is the canonical sitting URL, including the status query.
//
// Returns:
//   - string: /runs/{id} or /runs/{id}?status=...
func (m runModel) pagePath() string {
	return m.filterPage(m.Status)
}

// filterPage is the canonical URL for a status chip.
//
// Parameters:
//   - status: Case status filter. Empty means all.
//
// Returns:
//   - string: /runs/{id} or /runs/{id}?status=...
func (m runModel) filterPage(status string) string {
	return "/runs/" + url.PathEscape(m.Run.ID) + statusQuery(status)
}

// statusQuery builds a ?status= query or empty.
//
// Parameters:
//   - status: Case status filter.
//
// Returns:
//   - string: Query suffix.
func statusQuery(status string) string {
	if status == "" {
		return ""
	}

	return "?status=" + url.QueryEscape(status)
}
