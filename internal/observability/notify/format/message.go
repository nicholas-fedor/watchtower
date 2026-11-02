package format

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Template is a parsed notification template. Its legacy flag decides
// whether it renders the log entries alone or the full notification data.
type Template struct {
	parsed *template.Template // Parsed template.
	legacy bool               // Renders the log entries alone if true.
}

// New parses a notification template.
//
// A built-in template name selects that template. An empty string selects the
// default template, which is the default legacy template when legacy is true.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - tplString: Template text or built-in template name.
//   - legacy: Render the log entries alone if true.
//
// Returns:
//   - *Template: Parsed template.
//   - error: Non-nil if parsing fails, nil on success.
func New(log *zerolog.Logger, tplString string, legacy bool) (*Template, error) {
	if tplString == "" {
		return Default(legacy), nil
	}

	// Use common template if specified.
	builtin, found := commonTemplates[tplString]
	if found {
		log.Debug().Str("template", tplString).Msg("Using common template")
		tplString = builtin
	}

	tpl, err := template.New("").Funcs(templateFuncs).Parse(tplString)
	if err != nil {
		log.Debug().Err(err).Msg("Parse failed")

		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	return &Template{parsed: tpl, legacy: legacy}, nil
}

// Default returns the built-in default template. With legacy set, it returns
// the default legacy template. The built-in templates always parse.
//
// Parameters:
//   - legacy: Render the log entries alone if true.
//
// Returns:
//   - *Template: The default template.
func Default(legacy bool) *Template {
	key := "default"
	if legacy {
		key = "default-legacy"
	}

	tpl := template.Must(template.New("").Funcs(templateFuncs).Parse(commonTemplates[key]))

	return &Template{parsed: tpl, legacy: legacy}
}

// Legacy reports whether the template renders the log entries alone.
//
// Returns:
//   - bool: True for a legacy template.
func (m *Template) Legacy() bool {
	return m.legacy
}

// Render builds a notification message from data.
//
// A legacy template receives the log entries alone. Any other template
// receives the full data, including the report.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - data: Notification data.
//
// Returns:
//   - string: Rendered message.
//   - error: Non-nil if templating fails, nil on success.
func (m *Template) Render(log *zerolog.Logger, data Data) (string, error) {
	var body bytes.Buffer

	dataSource := any(data)
	if m.legacy {
		dataSource = data.Entries // Use entries only for legacy mode.
	}

	// Log template processing start with nil-safe report access.
	reportAvailable := data.Report != nil

	if log.GetLevel() <= zerolog.DebugLevel {
		log.Debug().
			Bool("legacy_template", m.legacy).
			Int("entries_count", len(data.Entries)).
			Int("container_count", reportCategoryCount(data.Report)).
			Bool("report_available", reportAvailable).
			Msg("Starting template processing for notification message")
	}

	// Execute template with data.
	err := m.parsed.Execute(&body, dataSource)
	if err != nil {
		log.Debug().
			Err(err).
			Bool("legacy_template", m.legacy).
			Str("template_name", m.parsed.Name()).
			Msg("Template execution failed")

		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	msg := body.String()

	// Log template processing result
	log.Debug().
		Int("msg_length", len(msg)).
		Bool("legacy_template", m.legacy).
		Str("template_name", m.parsed.Name()).
		Int("entries_count", len(data.Entries)).
		Msg("Template processing completed successfully")

	log.Debug().Str("message", msg).Msg("Generated notification message")

	return msg, nil
}

// reportCategoryCount returns the sum of report category lengths without calling All().
//
// Categories can overlap, so the sum may exceed the deduplicated All() count.
//
// Parameters:
//   - report: Session report, or nil.
//
// Returns:
//   - int: Combined length of all report categories.
func reportCategoryCount(report types.Report) int {
	if report == nil {
		return 0
	}

	return len(report.Scanned()) +
		len(report.Updated()) +
		len(report.Failed()) +
		len(report.Skipped()) +
		len(report.Stale()) +
		len(report.Fresh()) +
		len(report.Restarted())
}
