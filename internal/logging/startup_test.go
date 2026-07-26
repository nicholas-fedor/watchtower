package logging_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/logging"
)

// TestStartupLogging runs the Ginkgo test suite for the internal logging startup package.
func TestStartupLogging(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal Logging Startup Suite")
}

var _ = ginkgo.Describe("WriteStartupMessage", func() {
	var (
		client mockActions.MockClient
		buffer *bytes.Buffer
	)

	ginkgo.BeforeEach(func() {
		client = mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
		buffer = &bytes.Buffer{}
		logrus.SetOutput(buffer)
	})

	ginkgo.AfterEach(func() {
		logrus.SetOutput(logrus.StandardLogger().Out)
	})

	ginkgo.It("should log startup information with no notifier", func() {
		logging.WriteStartupMessage(logging.StartupParams{
			ScheduleInfo: logging.ScheduleInfo{HTTPAPIUpdate: true},
			Filtering:    "Watching all containers",
			Client:       client,
			Version:      "v1.0.0",
		})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Watchtower v1.0.0"))
		gomega.Expect(output).To(gomega.ContainSubstring("Using no notifications"))
	})

	ginkgo.It("should suppress startup messages when flag is set", func() {
		logging.WriteStartupMessage(logging.StartupParams{
			NoStartupMessage: true,
			Filtering:        "Watching all containers",
			Client:           client,
			Version:          "v1.0.0",
		})

		gomega.Expect(buffer.String()).To(gomega.BeEmpty())
	})

	ginkgo.It(
		"should suppress startup messages including HTTP API when no-startup-message is set",
		func() {
			logging.WriteStartupMessage(logging.StartupParams{
				NoStartupMessage: true,
				ScheduleInfo:     logging.ScheduleInfo{HTTPAPIUpdate: true},
				Filtering:        "Watching all containers",
				Client:           client,
				Version:          "v1.0.0",
			})

			gomega.Expect(buffer.String()).To(gomega.BeEmpty())
		},
	)

	ginkgo.It("should log scope information when provided", func() {
		logging.WriteStartupMessage(logging.StartupParams{
			Filtering: "Watching all containers",
			Scope:     "prod",
			Client:    client,
			Version:   "v1.0.0",
		})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Only checking containers in scope"))
	})

	ginkgo.It("should warn about trace logging", func() {
		originalLevel := logrus.GetLevel()

		logrus.SetLevel(logrus.TraceLevel)
		defer logrus.SetLevel(originalLevel)

		logging.WriteStartupMessage(logging.StartupParams{
			Filtering: "Watching all containers",
			Client:    client,
			Version:   "v1.0.0",
		})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Trace-level logging enabled"))
	})
})

var _ = ginkgo.Describe("SetupStartupLogger", func() {
	ginkgo.It("should return a standard logger entry when notifier is nil", func() {
		// Suppression is handled by WriteStartupMessage's early return; this helper
		// always builds a StandardLogger entry (not notifications.LocalLog).
		logger := logging.SetupStartupLogger(nil)
		gomega.Expect(logger).NotTo(gomega.BeNil())
		gomega.Expect(logger.Logger).To(gomega.Equal(logrus.StandardLogger()))
	})
})

var _ = ginkgo.Describe("LogNotifierInfo", func() {
	var buffer *bytes.Buffer

	ginkgo.BeforeEach(func() {
		buffer = &bytes.Buffer{}
		logrus.SetOutput(buffer)
	})

	ginkgo.AfterEach(func() {
		logrus.SetOutput(logrus.StandardLogger().Out)
	})

	ginkgo.It("should log configured notifiers", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		logging.LogNotifierInfo(logger, []string{"email", "slack"})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Using notifications: email, slack"))
	})

	ginkgo.It("should log when no notifiers are configured", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		logging.LogNotifierInfo(logger, []string{})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Using no notifications"))
	})
})

var _ = ginkgo.Describe("LogScheduleInfo", func() {
	var buffer *bytes.Buffer

	ginkgo.BeforeEach(func() {
		buffer = &bytes.Buffer{}
		logrus.SetOutput(buffer)
	})

	ginkgo.AfterEach(func() {
		logrus.SetOutput(logrus.StandardLogger().Out)
	})

	ginkgo.It("should log scheduled run information", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		sched := time.Now().Add(time.Hour)

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{Sched: sched})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Next scheduled run"))
	})

	ginkgo.It("should log one-time update", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{RunOnce: true})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Running a one time update"))
	})

	ginkgo.It("should log flag conflict when both run-once and update-on-start are set", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		updateOnStart := true

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{
			RunOnce:       true,
			UpdateOnStart: &updateOnStart,
		})

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("Run once mode: Disregarding update on start"))
	})

	ginkgo.It("should log update on start", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		updateOnStart := true

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{UpdateOnStart: &updateOnStart})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Update on startup enabled"))
	})

	ginkgo.It("should log HTTP API without periodic polls", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{HTTPAPIUpdate: true})

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("HTTP API enabled and periodic updates disabled"))
	})

	ginkgo.It("should log HTTP API with periodic polls", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{
			HTTPAPIUpdate:        true,
			HTTPAPIPeriodicPolls: true,
		})

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("HTTP API and periodic updates enabled"))
	})

	ginkgo.It("should log default periodic updates", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, logging.ScheduleInfo{})

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("Periodic updates are enabled with default schedule"))
	})
})
