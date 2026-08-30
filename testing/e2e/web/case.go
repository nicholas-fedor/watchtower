package web

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"net/url"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

const (
	// factorUnset is the cartesian "not set" level and is omitted from the assigned table.
	factorUnset = "unset"
)

// caseModel is one case drill-down page.
type caseModel struct {
	// Run is the parent sitting.
	Run store.Run
	// Case is the stored vector and documents.
	Case store.Case
	// Factors are assigned (non-unset) factor levels, sorted by name.
	Factors []kv
	// Verdict is expect vs inspect.
	Verdict caseVerdict
	// Changes are inspect fields that differed.
	Changes []inspectChange
	// LogRows are parsed Watchtower lines.
	LogRows []logRow
	// ExpectJSON is indented Expect.
	ExpectJSON string
	// InspectBeforeJSON is indented pre-session inspect.
	InspectBeforeJSON string
	// InspectAfterJSON is indented post-session inspect.
	InspectAfterJSON string
	// PorcelainJSON is indented porcelain when present.
	PorcelainJSON string
	// Err is a load failure.
	Err string
}

// kv is one named value for definition lists.
type kv struct {
	// Key is the factor or env name.
	Key string
	// Value is the assigned text.
	Value string
}

// handleCase serves GET /runs/:id/cases/:cid.
//
// Parameters:
//   - svc: Control plane.
//
// Returns:
//   - fiber.Handler: GET /runs/:id/cases/:cid.
func handleCase(svc *control.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		model := loadCase(c, svc)
		if htmxPartial(c) {
			return render(c, casePartial(model))
		}

		return render(c, casePage(model))
	}
}

// loadCase reads one case plus logs.
//
// Parameters:
//   - c: Fiber request context. Path id and cid are read.
//   - svc: Control plane.
//
// Returns:
//   - caseModel: Case documents and logs.
func loadCase(c fiber.Ctx, svc *control.Service) caseModel {
	runID := c.Params("id")
	caseID := c.Params("cid")
	model := caseModel{}

	run, runErr := svc.GetRun(c.Context(), runID)
	if runErr != nil {
		model.Err = runErr.Error()

		return model
	}

	model.Run = run

	item, caseErr := svc.GetCase(c.Context(), runID, caseID)
	if caseErr != nil {
		model.Err = caseErr.Error()

		return model
	}

	model.Case = item
	model.Factors = assignedFactors(item.Factors)
	model.Verdict = buildVerdict(item)
	model.Changes = diffInspect(item.InspectBefore, item.InspectAfter)
	model.ExpectJSON = prettyJSON(item.Expect)
	model.InspectBeforeJSON = prettyJSON(item.InspectBefore)
	model.InspectAfterJSON = prettyJSON(item.InspectAfter)
	model.PorcelainJSON = prettyJSON(item.Porcelain)

	lines, logErr := svc.QueryLogs(c.Context(), runID, caseID, "")
	if logErr != nil {
		model.Err = logErr.Error()
	} else {
		model.LogRows = parseLogs(lines)
	}

	return model
}

// signalLogs are info and above.
//
// Returns:
//   - []logRow: Non-debug lines.
func (m caseModel) signalLogs() []logRow {
	out := make([]logRow, 0, len(m.LogRows))
	for _, row := range m.LogRows {
		if !row.Noise {
			out = append(out, row)
		}
	}

	return out
}

// noiseLogs are debug and trace.
//
// Returns:
//   - []logRow: Debug/trace lines.
func (m caseModel) noiseLogs() []logRow {
	out := make([]logRow, 0, len(m.LogRows))
	for _, row := range m.LogRows {
		if row.Noise {
			out = append(out, row)
		}
	}

	return out
}

// shouldPoll reports whether the case fragment should keep polling.
//
// Returns:
//   - bool: True while the case is still running.
func (m caseModel) shouldPoll() bool {
	return m.Case.CaseID != "" && m.Case.Status == store.CaseRunning
}

// pagePath is the canonical case URL.
//
// Returns:
//   - string: /runs/{id}/cases/{cid}.
func (m caseModel) pagePath() string {
	return casePagePath(m.Run.ID, m.Case.CaseID)
}

// runPath is the parent sitting URL.
//
// Returns:
//   - string: /runs/{id}.
func (m caseModel) runPath() string {
	return "/runs/" + url.PathEscape(m.Run.ID)
}

// casePagePath builds a case drill-down URL.
//
// Parameters:
//   - runID: Sitting UUID.
//   - caseID: Case identifier.
//
// Returns:
//   - string: /runs/{id}/cases/{cid}.
func casePagePath(runID, caseID string) string {
	return "/runs/" + url.PathEscape(runID) + "/cases/" + url.PathEscape(caseID)
}

// assignedFactors returns map entries whose value is not empty or unset, sorted.
//
// Parameters:
//   - in: Factor or env map.
//
// Returns:
//   - []kv: Sorted pairs.
func assignedFactors(in map[string]string) []kv {
	out := make([]kv, 0, len(in))
	for key, value := range in {
		if value == "" || value == factorUnset {
			continue
		}

		out = append(out, kv{Key: key, Value: value})
	}

	slices.SortFunc(out, func(a, b kv) int {
		return strings.Compare(a.Key, b.Key)
	})

	return out
}

// prettyJSON indents a stored document.
//
// Parameters:
//   - raw: JSON bytes. Empty stays empty.
//
// Returns:
//   - string: Indented JSON, or the raw text when not JSON.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var value any

	err := jsonv2.Unmarshal(raw, &value)
	if err != nil {
		return string(raw)
	}

	var b strings.Builder

	writeErr := jsonv2.MarshalWrite(&b, value, jsontext.WithIndent("  "))
	if writeErr != nil {
		return string(raw)
	}

	return b.String()
}

// joinArgv formats Watchtower argv one argument per line.
//
// Parameters:
//   - argv: Argument vector.
//
// Returns:
//   - string: Newline-joined argv.
func joinArgv(argv []string) string {
	return strings.Join(argv, "\n")
}
