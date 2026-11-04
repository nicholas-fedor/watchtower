package notify

import (
	"context"
	"io"
	"log"
	"log/slog"

	"github.com/rs/zerolog"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// shoutrrrLogHandler is the slog handler behind the logger Watchtower gives the
// Shoutrrr sender.
//
// Shoutrrr logs its own records with the call that logs them as
// their source. It logs the logger service's notification text without one.
// A record with a source goes to the records logger at its own level. Text
// goes to the text logger unchanged. Shoutrrr's records then never mix with
// notification output written to stdout.
type shoutrrrLogHandler struct {
	records slog.Handler // Handler that logs Shoutrrr's records at their level.
	text    *log.Logger  // Logger that prints the logger service's text.
}

// shoutrrrLogPrefix starts each message Shoutrrr logs to Watchtower's log, so
// a line names the notification library it came from.
const shoutrrrLogPrefix = "Shoutrrr: "

// shoutrrrLogHandler routes the records of the Shoutrrr logger.
var _ slog.Handler = (*shoutrrrLogHandler)(nil)

// newShoutrrrLogger returns the logger Watchtower gives the Shoutrrr sender.
//
// Parameters:
//   - records: Logger that receives Shoutrrr's records at their level.
//   - text: Destination for the logger service's notification text.
//   - prefix: Prefix written before each line of text.
//
// Returns:
//   - *shoutrrrTypes.SlogLogger: Logger for the Shoutrrr sender.
func newShoutrrrLogger(records *zerolog.Logger, text io.Writer, prefix string) *shoutrrrTypes.SlogLogger {
	return shoutrrrTypes.NewSlogLogger(slog.New(&shoutrrrLogHandler{
		records: zerolog.NewSlogHandler(*records),
		text:    log.New(text, prefix, 0),
	}))
}

// Enabled reports whether the handler handles records at level.
//
// The info level is always handled, because the logger service's text arrives
// at it and is printed at every log level.
//
// Parameters:
//   - ctx: Context of the logging call.
//   - level: Level of the record.
//
// Returns:
//   - bool: True when a record at level is printed or logged.
func (h *shoutrrrLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level == slog.LevelInfo || h.records.Enabled(ctx, level)
}

// Handle prints a record without a source as text. It logs any other record
// at its level, with "Shoutrrr: " before the message.
//
// Parameters:
//   - ctx: Context of the logging call.
//   - record: Record to print or log.
//
// Returns:
//   - error: Error from writing the text or from the records handler.
func (h *shoutrrrLogHandler) Handle(ctx context.Context, record slog.Record) error {
	// Shoutrrr's SlogLogger documents that printed text is logged without a
	// source location. The logger service prints its notification text.
	if record.PC == 0 {
		return h.text.Output(1, record.Message) //nolint:wrapcheck // The text logger's write error needs no context.
	}

	record.Message = shoutrrrLogPrefix + record.Message

	return h.records.Handle(ctx, record) //nolint:wrapcheck // The records handler's error needs no context.
}

// WithAttrs returns a handler that adds attrs to Shoutrrr's records.
//
// Parameters:
//   - attrs: Attributes to add.
//
// Returns:
//   - slog.Handler: The new handler.
func (h *shoutrrrLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &shoutrrrLogHandler{records: h.records.WithAttrs(attrs), text: h.text}
}

// WithGroup returns a handler that nests the attributes of Shoutrrr's records
// under name.
//
// Parameters:
//   - name: Group name.
//
// Returns:
//   - slog.Handler: The new handler.
func (h *shoutrrrLogHandler) WithGroup(name string) slog.Handler {
	return &shoutrrrLogHandler{records: h.records.WithGroup(name), text: h.text}
}
