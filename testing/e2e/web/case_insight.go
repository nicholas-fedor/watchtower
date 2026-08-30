package web

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

const (
	shaPrefix     = "sha256:"
	shortSHALen   = 12
	logTimeLayout = "15:04:05"
)

// inspectSkip is docker inspect noise that never belongs on a case page.
var inspectSkip = map[string]struct{}{
	"AppArmorProfile":        {},
	"Driver":                 {},
	"ExecIDs":                {},
	"GraphDriver":            {},
	"HostnamePath":           {},
	"HostsPath":              {},
	"LogPath":                {},
	"MountLabel":             {},
	"Platform":               {},
	"ProcessLabel":           {},
	"ResolvConfPath":         {},
	"Path":                   {},
	"Args":                   {},
	"Mounts":                 {},
	"MaskedPaths":            {},
	"ReadonlyPaths":          {},
	"ConsoleSize":            {},
	"SandboxKey":             {},
	"SandboxID":              {},
	"EndpointID":             {},
	"NetworkID":              {},
	"HairpinMode":            {},
	"SecondaryIPAddresses":   {},
	"SecondaryIPv6Addresses": {},
	"LinkLocalIPv6Address":   {},
	"GlobalIPv6Address":      {},
	"IPv6Gateway":            {},
	"GlobalIPv6PrefixLen":    {},
	"LinkLocalIPv6PrefixLen": {},
	"DNSNames":               {},
	"IPAMConfig":             {},
	"DriverOpts":             {},
	"Links":                  {},
	"Aliases":                {},
	"GwPriority":             {},
}

// caseVerdict is the one-line story of the case.
type caseVerdict struct {
	// Expect is the derived outcome (updated, no-update, …).
	Expect string
	// ImageChanged is true when inspect Image sha differed.
	ImageChanged bool
	// Recreated is true when the container id changed.
	Recreated bool
	// ImageBefore is the shortened before image id.
	ImageBefore string
	// ImageAfter is the shortened after image id.
	ImageAfter string
	// Headline is the engineer-facing summary.
	Headline string
}

// inspectChange is one field that differed across inspect.
type inspectChange struct {
	// Path is a dotted inspect path.
	Path string
	// Before is the pre-session value.
	Before string
	// After is the post-session value.
	After string
}

// logRow is one Watchtower log line, parsed when the body is JSON.
type logRow struct {
	// Time is HH:MM:SS when known.
	Time string
	// Level is debug, info, warn, error, or empty.
	Level string
	// Message is the human line.
	Message string
	// Stream is stdout or stderr.
	Stream string
	// Noise is true for debug and trace.
	Noise bool
}

// buildVerdict summarizes expect vs inspect.
//
// Parameters:
//   - item: Stored case.
//
// Returns:
//   - caseVerdict: Headline and image/container facts.
func buildVerdict(item store.Case) caseVerdict {
	out := caseVerdict{
		Expect: expectOutcome(item.Expect),
	}
	before := inspectString(item.InspectBefore, "Image")
	after := inspectString(item.InspectAfter, "Image")
	out.ImageBefore = shortDigest(before)
	out.ImageAfter = shortDigest(after)
	out.ImageChanged = before != "" && after != "" && before != after
	out.Recreated = inspectString(item.InspectBefore, "Id") != "" &&
		inspectString(item.InspectAfter, "Id") != "" &&
		inspectString(item.InspectBefore, "Id") != inspectString(item.InspectAfter, "Id")

	switch {
	case item.Error != "":
		out.Headline = item.Error
	case out.Expect == "updated" && out.ImageChanged:
		out.Headline = "Image updated " + out.ImageBefore + " → " + out.ImageAfter
	case out.Expect == "updated" && !out.ImageChanged:
		out.Headline = "Expected an update, but the image id did not change"
	case out.ImageChanged:
		out.Headline = "Image changed " + out.ImageBefore + " → " + out.ImageAfter
	case out.Recreated:
		out.Headline = "Container recreated, but the image id is unchanged"
	default:
		out.Headline = "No image or container change"
	}

	return out
}

// diffInspect returns changed inspect leaves, skipping docker noise.
//
// Parameters:
//   - before: Pre-session inspect JSON.
//   - after: Post-session inspect JSON.
//
// Returns:
//   - []inspectChange: Paths that differed, sorted.
func diffInspect(before, after json.RawMessage) []inspectChange {
	left := unmarshalInspect(before)
	right := unmarshalInspect(after)
	if left == nil && right == nil {
		return nil
	}

	var out []inspectChange

	walkDiff("", left, right, &out)
	slices.SortFunc(out, func(a, b inspectChange) int {
		return strings.Compare(a.Path, b.Path)
	})

	return out
}

// parseLogs turns stored lines into display rows. JSON bodies become level+message.
//
// Parameters:
//   - lines: Loki or memory lines.
//
// Returns:
//   - []logRow: Parsed rows in ingest order.
func parseLogs(lines []stream.Line) []logRow {
	out := make([]logRow, 0, len(lines))
	for _, line := range lines {
		row := logRow{
			Stream:  line.Stream,
			Message: strings.TrimSpace(line.Body),
			Time:    line.Time.UTC().Format(logTimeLayout),
		}
		if parsed, ok := parseJSONLog(line.Body); ok {
			row = parsed
			row.Stream = line.Stream
			if row.Time == "" && !line.Time.IsZero() {
				row.Time = line.Time.UTC().Format(logTimeLayout)
			}
		} else if jsonNoise(line.Body) {
			row.Level = "debug"
			row.Noise = true
		}

		if row.Message == "" {
			continue
		}

		out = append(out, row)
	}

	return out
}

// parseJSONLog reads a zerolog-style line.
//
// Parameters:
//   - body: One log line.
//
// Returns:
//   - logRow: Parsed fields.
//   - bool: True when body was JSON with a message.
func parseJSONLog(body string) (logRow, bool) {
	var raw map[string]any

	err := jsonv2.Unmarshal([]byte(body), &raw)
	if err != nil {
		return logRow{}, false
	}

	msg, _ := raw["message"].(string)
	if msg == "" {
		msg, _ = raw["msg"].(string)
	}

	if msg == "" {
		return logRow{}, false
	}

	row := logRow{Message: msg}
	if level, ok := raw["level"].(string); ok {
		row.Level = level
		row.Noise = noiseLevel(level)
	}

	switch t := raw["time"].(type) {
	case string:
		if parsed, parseErr := time.Parse(time.RFC3339, t); parseErr == nil {
			row.Time = parsed.UTC().Format(logTimeLayout)
		} else {
			row.Time = t
		}
	}

	return row, true
}

func noiseLevel(level string) bool {
	switch strings.ToLower(level) {
	case "debug", "trace":
		return true
	default:
		return false
	}
}

func jsonNoise(body string) bool {
	trim := strings.TrimSpace(strings.Trim(body, ", \t"))
	switch trim {
	case "{", "}", "[", "]", "":
		return true
	}

	return strings.Contains(body, `"level":"debug"`) ||
		strings.Contains(body, `"level":"trace"`) ||
		strings.HasPrefix(trim, `"`) && strings.Contains(trim, `":`)
}

func unmarshalInspect(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}

	var value any

	err := jsonv2.Unmarshal(raw, &value)
	if err != nil {
		return string(raw)
	}

	return value
}

func walkDiff(path string, left, right any, out *[]inspectChange) {
	if _, skip := inspectSkip[lastPath(path)]; skip {
		return
	}

	lm, lok := asMap(left)
	rm, rok := asMap(right)
	if lok || rok {
		keys := make(map[string]struct{})
		maps.Copy(keys, keysOf(lm))
		maps.Copy(keys, keysOf(rm))
		names := slices.Sorted(maps.Keys(keys))
		for _, key := range names {
			walkDiff(joinPath(path, key), lm[key], rm[key], out)
		}

		return
	}

	ls, lslice := asStringSlice(left)
	rs, rslice := asStringSlice(right)
	if lslice && rslice {
		if !slices.Equal(ls, rs) {
			*out = append(*out, inspectChange{Path: path, Before: formatEnv(ls), After: formatEnv(rs)})
		}

		return
	}

	lv := scalar(left)
	rv := scalar(right)
	if lv == rv {
		return
	}

	*out = append(*out, inspectChange{
		Path:   path,
		Before: shortDigest(lv),
		After:  shortDigest(rv),
	})
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)

	return m, ok
}

func keysOf(m map[string]any) map[string]struct{} {
	out := make(map[string]struct{}, len(m))
	for key := range m {
		out[key] = struct{}{}
	}

	return out
}

func asStringSlice(v any) ([]string, bool) {
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			out = append(out, scalar(item))
		}

		return out, true
	default:
		return nil, false
	}
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}

		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		raw, err := jsonv2.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}

		return string(raw)
	}
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}

	return base + "." + key
}

func lastPath(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return path
	}

	return path[i+1:]
}

func formatEnv(values []string) string {
	return strings.Join(values, "\n")
}

func shortDigest(v string) string {
	if !strings.HasPrefix(v, shaPrefix) {
		return v
	}

	rest := v[len(shaPrefix):]
	if len(rest) > shortSHALen {
		return shaPrefix + rest[:shortSHALen]
	}

	return v
}

func inspectString(raw json.RawMessage, key string) string {
	var doc map[string]any

	err := jsonv2.Unmarshal(raw, &doc)
	if err != nil {
		return ""
	}

	return scalar(doc[key])
}

func expectOutcome(raw json.RawMessage) string {
	var doc struct {
		Outcome string `json:"outcome"`
	}

	err := jsonv2.Unmarshal(raw, &doc)
	if err != nil {
		return ""
	}

	return doc.Outcome
}
