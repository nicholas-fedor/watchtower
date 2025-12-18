package logging_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestStartupLogging runs the Ginkgo test suite for the internal logging startup package.
func TestStartupLogging(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal Logging Startup Suite")
}

var _ = ginkgo.Describe("WriteStartupMessage", func() {
	var (
		cmd    *cobra.Command
		client *mockTypes.MockClient
		buffer *bytes.Buffer
	)

	ginkgo.BeforeEach(func() {
		cmd = &cobra.Command{}
		client = mockTypes.NewMockClient(ginkgo.GinkgoT())
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

		client.EXPECT().GetVersion().Return("1.50")

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

	ginkgo.It(
		"should suppress startup messages including HTTP API when no-startup-message is set",
		func() {
			cmd.PersistentFlags().Bool("no-startup-message", true, "")
			cmd.PersistentFlags().Bool("http-api-update", true, "")

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

			// Should not log to buffer when suppressed, even with HTTP API enabled
			gomega.Expect(buffer.String()).To(gomega.BeEmpty())
		},
	)

	ginkgo.It("should log scope information when provided", func() {
		cmd.PersistentFlags().Bool("no-startup-message", false, "")
		cmd.PersistentFlags().Bool("http-api-update", false, "")

		client.EXPECT().GetVersion().Return("1.50")

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

		client.EXPECT().GetVersion().Return("1.50")

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

	ginkgo.It("should log host information when include-host-info flag is enabled", func() {
		cmd.PersistentFlags().Bool("no-startup-message", false, "")
		cmd.PersistentFlags().Bool("include-host-info", true, "")
		cmd.PersistentFlags().Bool("http-api-update", false, "")

		client.EXPECT().GetVersion().Return("1.50")
		client.EXPECT().GetInfo().Return(types.SystemInfo{
			OperatingSystem: "Ubuntu 20.04",
			OSType:          "linux",
			ServerVersion:   "20.10.0",
		}, nil)
		client.EXPECT().GetServerVersion().Return(types.VersionInfo{
			Version:       "20.10.0",
			KernelVersion: "5.4.0-42-generic",
			GoVersion:     "go1.16.3",
			Arch:          "amd64",
		}, nil)
		client.EXPECT().GetDiskUsage().Return(types.DiskUsage{
			LayersSize: 1024 * 1024 * 1024, // 1GB
			Images:     make([]types.ImageSummary, 5),
			Containers: make([]types.ContainerSummary, 3),
			Volumes:    make([]types.VolumeSummary, 2),
		}, nil)

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
		gomega.Expect(output).To(gomega.ContainSubstring("Host OS: Ubuntu 20.04 linux"))
		gomega.Expect(output).To(gomega.ContainSubstring("Docker Server Version: 20.10.0"))
		gomega.Expect(output).To(gomega.ContainSubstring("Kernel Version: 5.4.0-42-generic"))
		gomega.Expect(output).To(gomega.ContainSubstring("Docker Version: 20.10.0"))
		gomega.Expect(output).To(gomega.ContainSubstring("Go Version: go1.16.3"))
		gomega.Expect(output).To(gomega.ContainSubstring("Architecture: amd64"))
		gomega.Expect(output).
			To(gomega.ContainSubstring("Disk Usage: 1073741824 bytes used by 5 images, 3 containers, 2 volumes"))
	})

	ginkgo.It("should not log host information when include-host-info flag is disabled", func() {
		cmd.PersistentFlags().Bool("no-startup-message", false, "")
		cmd.PersistentFlags().Bool("include-host-info", false, "")
		cmd.PersistentFlags().Bool("http-api-update", false, "")

		client.EXPECT().GetVersion().Return("1.50")
		// Should not call GetInfo, GetServerVersion, or GetDiskUsage

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
		gomega.Expect(output).NotTo(gomega.ContainSubstring("Host OS:"))
		gomega.Expect(output).NotTo(gomega.ContainSubstring("Kernel Version:"))
		gomega.Expect(output).NotTo(gomega.ContainSubstring("Disk Usage:"))
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
