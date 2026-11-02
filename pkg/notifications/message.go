package notifications

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// buildMessage constructs a notification message from data.
//
// Parameters:
//   - data: Notification data.
//
// Returns:
//   - string: Rendered message.
//   - error: Non-nil if templating fails, nil on success.
func (n *shoutrrrTypeNotifier) buildMessage(data notificationData) (string, error) {
	log := n.ll()

	var body bytes.Buffer

	dataSource := any(data)
	if n.legacyTemplate {
		dataSource = data.Entries // Use entries only for legacy mode.
	}

	// Log template processing start with nil-safe report access.
	reportAvailable := data.Report != nil

	if log.GetLevel() <= zerolog.DebugLevel {
		log.Debug().
			Bool("legacy_template", n.legacyTemplate).
			Int("entries_count", len(data.Entries)).
			Int("container_count", reportCategoryCount(data.Report)).
			Bool("report_available", reportAvailable).
			Msg("Starting template processing for notification message")
	}

	// Execute template with data.
	err := n.template.Execute(&body, dataSource)
	if err != nil {
		log.Debug().
			Err(err).
			Bool("legacy_template", n.legacyTemplate).
			Str("template_name", n.template.Name()).
			Msg("Template execution failed")

		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	msg := body.String()

	// Log template processing result
	log.Debug().
		Int("msg_length", len(msg)).
		Bool("legacy_template", n.legacyTemplate).
		Str("template_name", n.template.Name()).
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

// getShoutrrrTemplate retrieves or generates a template.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - tplString: Template string.
//   - legacy: Use legacy mode if true.
//
// Returns:
//   - *template.Template: Parsed or default template.
//   - error: Non-nil if parsing fails, nil on success.
func getShoutrrrTemplate(log *zerolog.Logger, tplString string, legacy bool) (*template.Template, error) {
	tplBase := template.New("").Funcs(templateFuncs)

	// Use common template if specified.
	builtin, found := commonTemplates[tplString]
	if found {
		log.Debug().Str("template", tplString).Msg("Using common template")
		tplString = builtin
	}

	var tpl *template.Template

	var err error

	// Parse provided template or use default based on presence of tplString.
	switch {
	case tplString != "":
		// Parse provided template if non-empty.
		tpl, err = tplBase.Parse(tplString)
		if err != nil {
			log.Debug().Err(err).Msg("Parse failed")

			return nil, fmt.Errorf("failed to parse template: %w", err)
		}
	default:
		// Fall back to default template.
		key := "default"
		if legacy {
			key = "default-legacy"
		}

		tpl = template.Must(tplBase.Parse(commonTemplates[key]))
	}

	return tpl, nil
}
