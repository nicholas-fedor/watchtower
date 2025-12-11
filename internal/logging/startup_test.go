package logging_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	actionMocks "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/logging"
)

// TestStartupLogging runs the Ginkgo test suite for the internal logging startup package.
func TestStartupLogging(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal Logging Startup Suite")
}

var _ = ginkgo.Describe("WriteStartupMessage", func() {
	var (
		cmd    *cobra.Command
		client actionMocks.MockClient
		buffer *bytes.Buffer
	)

	ginkgo.BeforeEach(func() {
		cmd = &cobra.Command{}
		client = actionMocks.CreateMockClient(&actionMocks.TestData{}, false, false)
		buffer = &bytes.Buffer{}
		logrus.SetOutput(buffer)
	})

	ginkgo.AfterEach(func() {
		logrus.SetOutput(logrus.StandardLogger().Out)
	})

	ginkgo.It("should log startup information with no notifier", func() {
		cmd.PersistentFlags().Bool("no-startup-message", false, "")
		cmd.PersistentFlags().Bool("http-api-update", true, "")
		cmd.PersistentFlags().String("http-api-host", "", "")
		cmd.PersistentFlags().String("http-api-port", "8080", "")

		logging.WriteStartupMessage(
			cmd,
			time.Time{}, // no schedule
			"Watching all containers",
			"", // no scope
			client,
			nil, // no notifier
			"v1.0.0",
			nil, // read from flags
		)

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Watchtower v1.0.0"))
		gomega.Expect(output).To(gomega.ContainSubstring("Using no notifications"))
		gomega.Expect(output).To(gomega.ContainSubstring("The HTTP API is enabled"))
	})

	ginkgo.It("should suppress startup messages when flag is set", func() {
		cmd.PersistentFlags().Bool("no-startup-message", true, "")
		cmd.PersistentFlags().Bool("http-api-update", false, "")

		logging.WriteStartupMessage(
			cmd,
			time.Time{},
			"Watching all containers",
			"",
			client,
			nil,
			"v1.0.0",
			nil, // read from flags
		)

		// Should not log to buffer when suppressed
		gomega.Expect(buffer.String()).To(gomega.BeEmpty())
	})

	ginkgo.It("should log scope information when provided", func() {
		cmd.PersistentFlags().Bool("no-startup-message", false, "")
		cmd.PersistentFlags().Bool("http-api-update", false, "")

		logging.WriteStartupMessage(
			cmd,
			time.Time{},
			"Watching all containers",
			"prod",
			client,
			nil,
			"v1.0.0",
			nil, // read from flags
		)

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Only checking containers in scope"))
	})

	ginkgo.It("should warn about trace logging", func() {
		originalLevel := logrus.GetLevel()
		logrus.SetLevel(logrus.TraceLevel)
		defer logrus.SetLevel(originalLevel)

		cmd.PersistentFlags().Bool("no-startup-message", false, "")
		cmd.PersistentFlags().Bool("http-api-update", false, "")

		logging.WriteStartupMessage(
			cmd,
			time.Time{},
			"Watching all containers",
			"",
			client,
			nil,
			"v1.0.0",
			nil, // read from flags
		)

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Trace-level logging enabled"))
	})
})

var _ = ginkgo.Describe("SetupStartupLogger", func() {
	ginkgo.It("should return local log when startup messages are suppressed", func() {
		logger := logging.SetupStartupLogger(true, nil)
		gomega.Expect(logger).NotTo(gomega.BeNil())
	})

	ginkgo.It("should return logger when not suppressed", func() {
		logger := logging.SetupStartupLogger(false, nil)
		gomega.Expect(logger).NotTo(gomega.BeNil())
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

	ginkgo.It("should log multiple notifiers", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		logging.LogNotifierInfo(logger, []string{"slack", "email", "webhook"})

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("Using notifications: slack, email, webhook"))
	})

	ginkgo.It("should log no notifications when empty", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		logging.LogNotifierInfo(logger, []string{})

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Using no notifications"))
	})
})

var _ = ginkgo.Describe("LogScheduleInfo", func() {
	var (
		cmd    *cobra.Command
		buffer *bytes.Buffer
	)

	ginkgo.BeforeEach(func() {
		cmd = &cobra.Command{}
		buffer = &bytes.Buffer{}
		logrus.SetOutput(buffer)
	})

	ginkgo.AfterEach(func() {
		logrus.SetOutput(logrus.StandardLogger().Out)
	})

	ginkgo.It("should log scheduled run information", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())
		sched := time.Now().Add(time.Hour)

		logging.LogScheduleInfo(logger, cmd, sched, nil)

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Next scheduled run"))
	})

	ginkgo.It("should log one-time update", func() {
		cmd.PersistentFlags().Bool("run-once", true, "")
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, cmd, time.Time{}, nil)

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Running a one time update"))
	})

	ginkgo.It("should log flag conflict when both run-once and update-on-start are set", func() {
		cmd.PersistentFlags().Bool("run-once", true, "")
		cmd.PersistentFlags().Bool("update-on-start", true, "")
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, cmd, time.Time{}, nil)

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("Run once mode: Disregarding update on start"))
	})

	ginkgo.It("should log update on start", func() {
		cmd.PersistentFlags().Bool("update-on-start", true, "")
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, cmd, time.Time{}, nil)

		output := buffer.String()
		gomega.Expect(output).To(gomega.ContainSubstring("Update on startup enabled"))
	})

	ginkgo.It("should log HTTP API without periodic polls", func() {
		cmd.PersistentFlags().Bool("http-api-update", true, "")
		cmd.PersistentFlags().Bool("http-api-periodic-polls", false, "")
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, cmd, time.Time{}, nil)

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("HTTP API enabled and periodic updates disabled"))
	})

	ginkgo.It("should log HTTP API with periodic polls", func() {
		cmd.PersistentFlags().Bool("http-api-update", true, "")
		cmd.PersistentFlags().Bool("http-api-periodic-polls", true, "")
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, cmd, time.Time{}, nil)

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("HTTP API and periodic updates enabled"))
	})

	ginkgo.It("should log default periodic updates", func() {
		logger := logrus.NewEntry(logrus.StandardLogger())

		logging.LogScheduleInfo(logger, cmd, time.Time{}, nil)

		output := buffer.String()
		gomega.Expect(output).
			To(gomega.ContainSubstring("Periodic updates are enabled with default schedule"))
	})
})
