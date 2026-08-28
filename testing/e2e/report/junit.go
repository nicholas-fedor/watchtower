package report

import (
	"encoding/xml"
	"strconv"
)

// junitSuites is the JUnit XML document root.
type junitSuites struct {
	XMLName  xml.Name   `xml:"testsuites"`
	Tests    int        `xml:"tests,attr"`
	Failures int        `xml:"failures,attr"`
	Skipped  int        `xml:"skipped,attr"`
	Suite    junitSuite `xml:"testsuite"`
}

// junitSuite is one JUnit testsuite element.
type junitSuite struct {
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Skipped   int         `xml:"skipped,attr"`
	TestCases []junitCase `xml:"testcase"`
}

// junitCase is one JUnit testcase element.
type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

// junitFailure is a JUnit failure element.
type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// junitSkipped is a JUnit skipped element.
type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// JUnit renders a JUnit XML document for the sitting.
//
// Parameters:
//   - summary: Aggregated results.
//
// Returns:
//   - string: XML document.
func JUnit(summary Summary) string {
	suite := junitSuite{
		Name:     "watchtower-e2e",
		Tests:    len(summary.Cases),
		Failures: summary.Failed,
		Skipped:  summary.Skipped,
	}

	for _, result := range summary.Cases {
		item := junitCase{
			Name:      result.CaseID,
			Classname: "e2e",
			Time:      strconv.FormatInt(result.Duration, 10),
		}
		if result.Skipped {
			item.Skipped = &junitSkipped{Message: result.Err}
		} else if !result.Passed {
			item.Failure = &junitFailure{Message: result.Status, Body: result.Err}
		}

		suite.TestCases = append(suite.TestCases, item)
	}

	doc := junitSuites{
		Tests:    suite.Tests,
		Failures: suite.Failures,
		Skipped:  suite.Skipped,
		Suite:    suite,
	}

	raw, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ""
	}

	return xml.Header + string(raw)
}
