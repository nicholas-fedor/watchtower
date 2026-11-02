package notifications

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
)

// eventFieldRawCapacity is the initial close-brace buffer size for eventFieldMap.
const eventFieldRawCapacity = 256

// eventFieldRawMaxCapacity is the largest pooled close-brace buffer that is reused.
const eventFieldRawMaxCapacity = 8 << 10

// eventFieldRawPool reuses close-brace JSON buffers across hooked log events.
var eventFieldRawPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, eventFieldRawCapacity)

		return &buf
	},
}

// eventFieldMap extracts application fields and timestamp from a zerolog.Event's
// in-progress buffer.
//
// At hook time the buffer holds a partial JSON object (no message, no closing brace).
// Zerolog does not expose a public GetString API on Event. This helper reconstructs
// the field map from the unexported buffer via reflection. The field layout is
// pinned to zerolog v1.35.x and requires revalidation on upgrades. reflect and
// unsafe are required because the event buffer is unexported.
//
// Parameters:
//   - event: In-progress zerolog event, or nil.
//
// Returns:
//   - map[string]any: Application fields with standard envelope keys removed.
//   - time.Time: Event timestamp, or time.Now when missing or unparsable.
//   - bool: False when extraction fails (caller must fail-closed).
func eventFieldMap(event *zerolog.Event) (map[string]any, time.Time, bool) {
	now := time.Now()
	if event == nil {
		return nil, now, false
	}

	rv := reflect.ValueOf(event).Elem().FieldByName("buf")
	if !rv.IsValid() || rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return nil, now, false
	}

	buf := unsafe.Slice((*byte)(rv.UnsafePointer()), rv.Len())

	rawPtr, ok := eventFieldRawPool.Get().(*[]byte)
	if !ok || rawPtr == nil {
		buf := make([]byte, 0, eventFieldRawCapacity)
		rawPtr = &buf
	}

	// Reset the pooled buffer without dropping its capacity.
	raw := (*rawPtr)[:0]

	if cap(raw) < len(buf)+1 {
		raw = make([]byte, 0, len(buf)+1)
	}

	// Close the partial JSON object produced before the message is appended.
	raw = append(raw, buf...)
	raw = append(raw, '}')

	var fieldMap map[string]any

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	err := dec.Decode(&fieldMap)

	// Return only bounded buffers so oversized events do not stay in the pool.
	if cap(raw) <= eventFieldRawMaxCapacity {
		*rawPtr = raw[:0]
		eventFieldRawPool.Put(rawPtr)
	}

	if err != nil {
		return nil, now, false
	}

	// Normalize json.Number values to int64 or float64 so templates can compare
	// them as numbers without precision loss for large integer values.
	for fieldKey, fieldVal := range fieldMap {
		num, ok := fieldVal.(json.Number)
		if !ok {
			continue
		}

		intVal, intErr := num.Int64()
		if intErr == nil {
			fieldMap[fieldKey] = intVal

			continue
		}

		floatVal, floatErr := num.Float64()
		if floatErr == nil {
			fieldMap[fieldKey] = floatVal
		}
	}

	timestamp := now

	rawTS, ok := fieldMap[zerolog.TimestampFieldName]
	if ok {
		tsStr, ok := rawTS.(string)
		if ok {
			parsed, parseErr := time.Parse(time.RFC3339, tsStr)
			if parseErr != nil {
				parsed, parseErr = time.Parse(time.RFC3339Nano, tsStr)
			}

			if parseErr == nil {
				timestamp = parsed
			}
		}
	}

	// Drop standard zerolog envelope keys so templates see application fields only.
	// Keep ErrorFieldName ("error"): templates and JSON historically surface
	// WithError/Err as Data["error"] (historical notification template field name).
	delete(fieldMap, zerolog.LevelFieldName)
	delete(fieldMap, zerolog.TimestampFieldName)
	delete(fieldMap, zerolog.MessageFieldName)
	delete(fieldMap, zerolog.CallerFieldName)

	return fieldMap, timestamp, true
}
