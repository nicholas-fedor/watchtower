// Package notifications provides mechanisms for sending notifications via various services.
// This file contains tests for Shoutrrr notification functionality, including templating and batching.
package notifications

import (
	"text/template"
	"time"

	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/notifications/templates"
	"github.com/nicholas-fedor/watchtower/pkg/session"
)

var allButTrace = logrus.DebugLevel

var legacyMockData = Data{
	Entries: []*logrus.Entry{
		{
			Level:   logrus.InfoLevel,
			Message: "foo Bar",
		},
	},
}

var mockDataMultipleEntries = Data{
	Entries: []*logrus.Entry{
		{
			Level:   logrus.InfoLevel,
			Message: "The situation is under control",
		},
		{
			Level:   logrus.WarnLevel,
			Message: "All the smoke might be covering up some problems",
		},
		{
			Level:   logrus.ErrorLevel,
			Message: "Turns out everything is on fire",
		},
	},
}

var mockDataAllFresh = Data{
	Entries: []*logrus.Entry{},
	Report:  mocks.CreateMockProgressReport(session.FreshState),
}

// mockDataFromStates generates mock notification data with specified container states.
// It includes legacy log entries and static data for testing purposes.
func mockDataFromStates(states ...session.State) Data {
	hostname := "Mock"
	prefix := ""

	return Data{
		Entries: legacyMockData.Entries,
		Report:  mocks.CreateMockProgressReport(states...),
		StaticData: StaticData{
			Title: GetTitle(hostname, prefix),
			Host:  hostname,
		},
	}
}

var _ = ginkgo.Describe("Shoutrrr", func() {
	var logBuffer *gbytes.Buffer

	// BeforeEach configures the global logrus instance for each test.
	ginkgo.BeforeEach(func() {
		logBuffer = gbytes.NewBuffer()
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.TraceLevel)
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors:    true,
			DisableTimestamp: true,
		})
	})

	ginkgo.When("passing a common template name", func() {
		ginkgo.It("should format using that template", func() {
			expected := `
updt1 (mock/updt1:latest): Updated
`[1:]
			data := mockDataFromStates(session.UpdatedState)
			gomega.Expect(getTemplatedResult(`porcelain.v1.summary-no-log`, false, data)).
				To(gomega.Equal(expected))
		})
	})

	ginkgo.When("adding a log hook", func() {
		ginkgo.When("it has not been added before", func() {
			ginkgo.It("should be added to the logrus hooks", func() {
				level := logrus.TraceLevel
				hooksBefore := len(logrus.StandardLogger().Hooks[level])
				shoutrrr := createNotifier(
					[]string{},
					level,
					"",
					true,
					StaticData{},
					false,
					time.Second,
				)
				shoutrrr.AddLogHook()
				hooksAfter := len(logrus.StandardLogger().Hooks[level])
				gomega.Expect(hooksAfter).To(gomega.BeNumerically(">", hooksBefore))
			})
		})
		ginkgo.When("it is being added a second time", func() {
			ginkgo.It("should not be added to the logrus hooks", func() {
				level := logrus.TraceLevel
				shoutrrr := createNotifier(
					[]string{},
					level,
					"",
					true,
					StaticData{},
					false,
					time.Second,
				)
				shoutrrr.AddLogHook()
				hooksBefore := len(logrus.StandardLogger().Hooks[level])
				shoutrrr.AddLogHook()
				hooksAfter := len(logrus.StandardLogger().Hooks[level])
				gomega.Expect(hooksAfter).To(gomega.Equal(hooksBefore))
			})
		})
	})

	ginkgo.When("using legacy templates", func() {
		ginkgo.When("no custom template is provided", func() {
			ginkgo.It("should format the messages using the default template", func() {
				cmd := new(cobra.Command)
				flags.RegisterNotificationFlags(cmd)

				shoutrrr := createNotifier(
					[]string{},
					logrus.TraceLevel,
					"",
					true,
					StaticData{},
					false,
					time.Second,
				)
				entries := []*logrus.Entry{
					{Message: "foo bar"},
				}

				s, err := shoutrrr.buildMessage(Data{Entries: entries})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(s).To(gomega.Equal("foo bar"))
			})
		})
		ginkgo.When("given a valid custom template", func() {
			ginkgo.It("should format the messages using the custom template", func() {
				tplString := `{{range .}}{{.Level}}: {{.Message}}{{println}}{{end}}`
				tpl, err := getShoutrrrTemplate(tplString, true)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				shoutrrr := &shoutrrrTypeNotifier{
					template:       tpl,
					legacyTemplate: true,
				}

				entries := []*logrus.Entry{
					{
						Level:   logrus.InfoLevel,
						Message: "foo bar",
					},
				}

				s, err := shoutrrr.buildMessage(Data{Entries: entries})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(s).To(gomega.Equal("info: foo bar\n"))
			})
		})

		ginkgo.Describe("the default template", func() {
			ginkgo.When("all containers are fresh", func() {
				ginkgo.It("should return an empty string", func() {
					gomega.Expect(getTemplatedResult(``, true, mockDataAllFresh)).
						To(gomega.Equal(""))
				})
			})
		})

		ginkgo.When("given an invalid custom template", func() {
			ginkgo.It("should format the messages using the default template", func() {
				invNotif, err := createNotifierWithTemplate(`{{ intentionalSyntaxError`, true)
				gomega.Expect(err).To(gomega.HaveOccurred())
				invMsg, err := invNotif.buildMessage(legacyMockData)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				defNotif, err := createNotifierWithTemplate(``, true)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defMsg, err := defNotif.buildMessage(legacyMockData)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(invMsg).To(gomega.Equal(defMsg))
			})
		})

		ginkgo.When("given a template that is using ToUpper function", func() {
			ginkgo.It("should return the text in UPPER CASE", func() {
				tplString := `{{range .}}{{ .Message | ToUpper }}{{end}}`
				gomega.Expect(getTemplatedResult(tplString, true, legacyMockData)).
					To(gomega.Equal("FOO BAR"))
			})
		})

		ginkgo.When("given a template that is using ToLower function", func() {
			ginkgo.It("should return the text in lower case", func() {
				tplString := `{{range .}}{{ .Message | ToLower }}{{end}}`
				gomega.Expect(getTemplatedResult(tplString, true, legacyMockData)).
					To(gomega.Equal("foo bar"))
			})
		})

		ginkgo.When("given a template that is using Title function", func() {
			ginkgo.It("should return the text in Title Case", func() {
				tplString := `{{range .}}{{ .Message | Title }}{{end}}`
				gomega.Expect(getTemplatedResult(tplString, true, legacyMockData)).
					To(gomega.Equal("Foo Bar"))
			})
		})
	})

	ginkgo.When("using report templates", func() {
		ginkgo.When("no custom template is provided", func() {
			ginkgo.It("should format the messages using the default template", func() {
				expected := `4 Scanned, 2 Updated, 1 Failed
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000
- updt2 (mock/updt2:latest): 01d120000000 updated to d0a120000000
- frsh1 (mock/frsh1:latest): Fresh
- skip1 (mock/skip1:latest): Skipped: unpossible
- fail1 (mock/fail1:latest): Failed: accidentally the whole container`
				data := mockDataFromStates(
					session.UpdatedState,
					session.FreshState,
					session.FailedState,
					session.SkippedState,
					session.UpdatedState,
				)
				gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
			})
		})

		ginkgo.When("using a template referencing Title", func() {
			ginkgo.It("should contain the title in the output", func() {
				expected := `Watchtower updates on Mock`
				data := mockDataFromStates(session.UpdatedState)
				gomega.Expect(getTemplatedResult(`{{ .Title }}`, false, data)).
					To(gomega.Equal(expected))
			})
		})

		ginkgo.When("using a template referencing Host", func() {
			ginkgo.It("should contain the hostname in the output", func() {
				expected := `Mock`
				data := mockDataFromStates(session.UpdatedState)
				gomega.Expect(getTemplatedResult(`{{ .Host }}`, false, data)).
					To(gomega.Equal(expected))
			})
		})

		ginkgo.Describe("the default template", func() {
			ginkgo.When("all containers are fresh", func() {
				ginkgo.It("should return the summary", func() {
					gomega.Expect(getTemplatedResult(``, false, mockDataAllFresh)).
						To(gomega.Equal("1 Scanned, 0 Updated, 0 Failed"))
				})
			})
			ginkgo.When("at least one container was updated", func() {
				ginkgo.It("should send a report", func() {
					expected := `1 Scanned, 1 Updated, 0 Failed
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000`
					data := mockDataFromStates(session.UpdatedState)
					gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
				})
			})
			ginkgo.When("at least one container failed to update", func() {
				ginkgo.It("should send a report", func() {
					expected := `1 Scanned, 0 Updated, 1 Failed
- fail1 (mock/fail1:latest): Failed: accidentally the whole container`
					data := mockDataFromStates(session.FailedState)
					gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
				})
			})
			ginkgo.When("the report is nil", func() {
				ginkgo.It("should return the logged entries", func() {
					expected := `The situation is under control
All the smoke might be covering up some problems
Turns out everything is on fire
`
					gomega.Expect(getTemplatedResult(``, false, mockDataMultipleEntries)).
						To(gomega.Equal(expected))
				})
			})
		})
	})

	ginkgo.When("batching notifications", func() {
		ginkgo.When("no messages are queued", func() {
			ginkgo.It("should not send any notification", func() {
				shoutrrr := createNotifier(
					[]string{"logger://"},
					allButTrace,
					"",
					true,
					StaticData{},
					false,
					time.Duration(0),
				)
				shoutrrr.StartNotification()
				shoutrrr.SendNotification(nil)
				gomega.Consistently(logBuffer).ShouldNot(gbytes.Say(`Shoutrrr:`))
			})
		})
		ginkgo.When("at least one message is queued", func() {
			ginkgo.It("should send a notification", func() {
				shoutrrr := createNotifier(
					[]string{"logger://"},
					allButTrace,
					"",
					true,
					StaticData{},
					false,
					time.Duration(0),
				)
				shoutrrr.AddLogHook()
				shoutrrr.StartNotification()
				logrus.Info("This log message is sponsored by ContainrrrVPN")
				shoutrrr.SendNotification(nil)
				gomega.Eventually(logBuffer).
					Should(gbytes.Say(`Shoutrrr: This log message is sponsored by ContainrrrVPN`))
			})
		})
	})

	ginkgo.When("the title data field is empty", func() {
		ginkgo.It("should not have set the title param", func() {
			shoutrrr := createNotifier([]string{"logger://"}, allButTrace, "", true, StaticData{
				Host:  "test.host",
				Title: "",
			}, false, time.Second)
			_, found := shoutrrr.params.Title()
			gomega.Expect(found).ToNot(gomega.BeTrue())
		})
	})

	ginkgo.When("sending notifications", func() {
		ginkgo.It("SlowNotificationNotSent", func() {
			_, blockingRouter := sendNotificationsWithBlockingRouter(true)

			gomega.Eventually(blockingRouter.sent).Should(gomega.Not(gomega.Receive()))
		})

		ginkgo.It("SlowNotificationSent", func() {
			shoutrrr, blockingRouter := sendNotificationsWithBlockingRouter(true)

			blockingRouter.unlock <- true
			shoutrrr.Close()

			gomega.Eventually(blockingRouter.sent).Should(gomega.Receive(gomega.BeTrue()))
		})
	})
})

// blockingRouter simulates a notification router with blocking behavior for testing.
// It waits for an unlock signal before sending and signals completion via a channel.
type blockingRouter struct {
	unlock chan bool
	sent   chan bool
}

func (b blockingRouter) Send(_ string, _ *types.Params) []error {
	<-b.unlock

	b.sent <- true

	return nil
}

// sendNotificationsWithBlockingRouter creates a notifier with a blocking router for testing.
// It queues a message and returns the notifier and router to verify notification delays.
func sendNotificationsWithBlockingRouter(legacy bool) (*shoutrrrTypeNotifier, *blockingRouter) {
	router := &blockingRouter{
		unlock: make(chan bool, 1),
		sent:   make(chan bool, 1),
	}

	tpl, err := getShoutrrrTemplate("", legacy)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	shoutrrr := &shoutrrrTypeNotifier{
		template:       tpl,
		messages:       make(chan string, 1),
		done:           make(chan bool),
		Router:         router,
		legacyTemplate: legacy,
		params:         &types.Params{},
		delay:          time.Duration(0),
	}

	entry := &logrus.Entry{
		Message: "foo bar",
	}

	go sendNotifications(shoutrrr)

	shoutrrr.StartNotification()
	_ = shoutrrr.Fire(entry)
	shoutrrr.SendNotification(nil)

	return shoutrrr, router
}

// createNotifierWithTemplate creates a notifier with a specified template for testing.
// It returns the notifier and an error, falling back to a default template if parsing fails.
func createNotifierWithTemplate(tplString string, legacy bool) (*shoutrrrTypeNotifier, error) {
	tpl, err := getShoutrrrTemplate(tplString, legacy)
	if err != nil {
		logrus.Errorf(
			"Could not use configured notification template: %s. Using default template",
			err,
		)

		tplBase := template.New("").Funcs(templates.Funcs)

		defaultKey := "default"
		if legacy {
			defaultKey = "default-legacy"
		}

		tpl = template.Must(tplBase.Parse(commonTemplates[defaultKey]))
		// Do not reset err; keep it to indicate the original parsing failure
	}

	return &shoutrrrTypeNotifier{
		template:       tpl,
		legacyTemplate: legacy,
	}, err
}

// getTemplatedResult generates a templated message for testing.
// It builds and returns the message string, expecting no errors.
func getTemplatedResult(tplString string, legacy bool, data Data) string {
	notifier, err := createNotifierWithTemplate(tplString, legacy)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())
	msg, err := notifier.buildMessage(data)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())

	return msg
}
