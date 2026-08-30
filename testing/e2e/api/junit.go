package api

import (
	"fmt"
	"html"
	"strings"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// msPerSecond converts stored duration_ms to JUnit seconds.
const msPerSecond = 1000.0

// junitXML renders a sitting as JUnit XML.
//
// Parameters:
//   - run: Sitting snapshot.
//   - cases: Cases to include.
//
// Returns:
//   - string: XML document.
func junitXML(run store.Run, cases []store.Case) string {
	var b strings.Builder

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<testsuite name="watchtower-e2e-%s" tests="%d" failures="%d" skipped="%d">`+"\n",
		html.EscapeString(run.Label), run.Passed+run.Failed+run.Skipped, run.Failed, run.Skipped)

	for _, item := range cases {
		writeJUnitCase(&b, item)
	}

	b.WriteString("</testsuite>\n")

	return b.String()
}

// writeJUnitCase appends one testcase element.
//
// Parameters:
//   - b: XML builder.
//   - item: Case row.
func writeJUnitCase(b *strings.Builder, item store.Case) {
	fmt.Fprintf(b, `  <testcase name="%s" time="%.3f">`, html.EscapeString(item.CaseID), float64(item.DurationMs)/msPerSecond)

	switch item.Status {
	case store.CaseFail:
		fmt.Fprintf(b, `<failure message="%s"/>`, html.EscapeString(item.Error))
	case store.CaseSkip, store.CaseInterrupted:
		b.WriteString(`<skipped/>`)
	case store.CasePass, store.CasePending, store.CaseRunning:
	}

	b.WriteString("</testcase>\n")
}
