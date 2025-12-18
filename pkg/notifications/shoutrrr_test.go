// Package notifications provides mechanisms for sending notifications via various services.
// This file contains tests for Shoutrrr notification functionality, including templating and batching.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"text/template"
	"time"

	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
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
	Report:  mockActions.CreateMockProgressReport(session.FreshState),
}

// mockDataFromStates generates mock notification data with specified container states.
// It includes legacy log entries and static data for testing purposes.
func mockDataFromStates(states ...session.State) Data {
	hostname := "Mock"
	prefix := ""

	return Data{
		Entries: legacyMockData.Entries,
		Report:  mockActions.CreateMockProgressReport(states...),
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
				expected := `4 Scanned, 2 Updated, 0 Restarted, 1 Failed
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
			ginkgo.It("should use image IDs for container update reporting", func() {
				data := mockDataFromStates(session.UpdatedState)
				result := getTemplatedResult(``, false, data)

				// Verify that the result contains image ID formats, not container IDs
				// Image IDs in the mock data are like "01d110000000" and "d0a110000000"
				// Container IDs are like "c79110000000"
				gomega.Expect(result).To(gomega.ContainSubstring("01d110000000"))
				gomega.Expect(result).To(gomega.ContainSubstring("d0a110000000"))
				gomega.Expect(result).NotTo(gomega.ContainSubstring("c79110000000"))
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
					expected := `1 Scanned, 0 Updated, 0 Restarted, 0 Failed
- frsh1 (mock/frsh1:latest): Fresh`
					gomega.Expect(getTemplatedResult(``, false, mockDataAllFresh)).
						To(gomega.Equal(expected))
				})
			})
			ginkgo.When("at least one container was updated", func() {
				ginkgo.It("should send a report", func() {
					expected := `1 Scanned, 1 Updated, 0 Restarted, 0 Failed
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000`
					data := mockDataFromStates(session.UpdatedState)
					gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
				})
			})
			ginkgo.When("at least one container failed to update", func() {
				ginkgo.It("should send a report", func() {
					expected := `1 Scanned, 0 Updated, 0 Restarted, 1 Failed
- fail1 (mock/fail1:latest): Failed: accidentally the whole container`
					data := mockDataFromStates(session.FailedState)
					gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
				})
			})
			ginkgo.When("containers are restarted due to dependencies", func() {
				ginkgo.It("should send a report with restarted containers", func() {
					expected := `2 Scanned, 1 Updated, 1 Restarted, 0 Failed
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000
- rstr1 (mock/rstr1:latest): Restarted`
					data := mockDataFromStates(session.UpdatedState, session.RestartedState)
					gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
				})
			})
			ginkgo.When("mixing updated and restarted containers", func() {
				ginkgo.It("should show different states for updated vs restarted", func() {
					expected := `3 Scanned, 2 Updated, 1 Restarted, 0 Failed
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000
- updt2 (mock/updt2:latest): 01d120000000 updated to d0a120000000
- rstr1 (mock/rstr1:latest): Restarted`
					data := mockDataFromStates(
						session.UpdatedState,
						session.RestartedState,
						session.UpdatedState,
					)
					gomega.Expect(getTemplatedResult(``, false, data)).To(gomega.Equal(expected))
				})
			})
			ginkgo.When("testing JSON output format", func() {
				ginkgo.It("should include restarted containers in JSON response", func() {
					data := mockDataFromStates(session.UpdatedState, session.RestartedState)
					jsonResult := getTemplatedResult(`{{ . | ToJSON }}`, false, data)

					var result map[string]any
					gomega.Expect(json.Unmarshal([]byte(jsonResult), &result)).To(gomega.Succeed())

					report, ok := result["report"].(map[string]any)
					gomega.Expect(ok).To(gomega.BeTrue())

					updated, ok := report["updated"].([]any)
					gomega.Expect(ok).To(gomega.BeTrue())
					gomega.Expect(updated).To(gomega.HaveLen(1))

					restarted, ok := report["restarted"].([]any)
					gomega.Expect(ok).To(gomega.BeTrue())
					gomega.Expect(restarted).To(gomega.HaveLen(1))
					gomega.Expect(restarted[0]).To(gomega.HaveKey("state"))
					gomega.Expect(restarted[0].(map[string]any)["state"]).
						To(gomega.Equal("Restarted"))
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
				shoutrrr.StartNotification(false)
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
				shoutrrr.StartNotification(false)
				logrus.Info("This log message is sponsored by Watchtower")
				shoutrrr.SendNotification(nil)
				gomega.Eventually(logBuffer).
					Should(gbytes.Say(`Shoutrrr: This log message is sponsored by Watchtower`))
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

	ginkgo.When("sending notifications with error handling", func() {
		ginkgo.It("should handle index guard when errs length exceeds URLs length", func() {
			mockRouter := &mockRouter{
				sendErrors: []error{errors.New("test error"), errors.New("extra error")},
			}

			shoutrrr := createNotifier(
				[]string{"logger://"},
				allButTrace,
				"",
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.Router = mockRouter
			shoutrrr.AddLogHook()
			logrus.Info("test message")

			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)

			shoutrrr.Close()
			gomega.Eventually(logBuffer).Should(gbytes.Say(`index_mismatch`))
		})

		ginkgo.It("should categorize authentication errors", func() {
			mockRouter := &mockRouter{
				sendErrors: []error{errors.New("unauthorized access")},
			}

			shoutrrr := createNotifier(
				[]string{"logger://"},
				allButTrace,
				"",
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.Router = mockRouter
			shoutrrr.AddLogHook()
			logrus.Info("test message")

			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)

			shoutrrr.Close()
			gomega.Eventually(logBuffer).Should(gbytes.Say(`failure_type.*authentication`))
		})

		ginkgo.It("should categorize network errors", func() {
			mockRouter := &mockRouter{
				sendErrors: []error{errors.New("connection timeout")},
			}

			shoutrrr := createNotifier(
				[]string{"logger://"},
				allButTrace,
				"",
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.Router = mockRouter
			shoutrrr.AddLogHook()
			logrus.Info("test message")

			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)

			shoutrrr.Close()
			gomega.Eventually(logBuffer).Should(gbytes.Say(`failure_type.*network`))
		})

		ginkgo.It("should categorize rate limit errors", func() {
			mockRouter := &mockRouter{
				sendErrors: []error{errors.New("too many requests")},
			}

			shoutrrr := createNotifier(
				[]string{"logger://"},
				allButTrace,
				"",
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.Router = mockRouter
			shoutrrr.AddLogHook()
			logrus.Info("test message")

			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)

			shoutrrr.Close()
			gomega.Eventually(logBuffer).Should(gbytes.Say(`failure_type.*rate_limit`))
		})

		ginkgo.It("should categorize unknown errors", func() {
			mockRouter := &mockRouter{
				sendErrors: []error{errors.New("some unknown error")},
			}

			shoutrrr := createNotifier(
				[]string{"logger://"},
				allButTrace,
				"",
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.Router = mockRouter
			shoutrrr.AddLogHook()
			shoutrrr.StartNotification(false)
			logrus.Info("test message")

			shoutrrr.SendNotification(nil)

			shoutrrr.Close()
			gomega.Eventually(logBuffer).Should(gbytes.Say(`failure_type.*unknown`))
		})

		ginkgo.It("should log summary with failure counts", func() {
			mockRouter := &mockRouter{
				sendErrors: []error{errors.New("auth error"), errors.New("network error")},
			}

			shoutrrr := createNotifier(
				[]string{"logger://", "logger://"},
				allButTrace,
				"",
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.Router = mockRouter
			shoutrrr.AddLogHook()
			logrus.Info("test message")

			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)

			shoutrrr.Close()
			gomega.Eventually(logBuffer).Should(gbytes.Say(`failed_count.*2`))
		})
	})

	ginkgo.When("closing the notifier", func() {
		ginkgo.When("Close() is called multiple times", func() {
			ginkgo.It("should be idempotent and not panic", func() {
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

				// First close should work normally
				shoutrrr.Close()

				// Subsequent closes should be no-ops
				shoutrrr.Close()
				shoutrrr.Close()

				// Should not panic
			})
		})

		ginkgo.When("Close() is called without starting the goroutine", func() {
			ginkgo.It("should not panic or block", func() {
				shoutrrr := createNotifier(
					[]string{"logger://"},
					allButTrace,
					"",
					true,
					StaticData{},
					false,
					time.Duration(0),
				)
				// Note: Not calling AddLogHook(), so no goroutine is started

				// Close should work without blocking
				shoutrrr.Close()

				// Should not panic
			})
		})

		ginkgo.When("operations are attempted after Close()", func() {
			ginkgo.It("should handle gracefully without panicking", func() {
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

				// Close the notifier
				shoutrrr.Close()

				// These operations should not panic after close
				shoutrrr.StartNotification(false)
				shoutrrr.SendNotification(nil)
				shoutrrr.SendFilteredEntries([]*logrus.Entry{}, nil)

				// Fire should still work (but may not send if channel is closed)
				entry := &logrus.Entry{Message: "test"}
				err := shoutrrr.Fire(entry)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// Should not panic
			})
		})

		ginkgo.When("Close() is called concurrently", func() {
			ginkgo.It("should handle concurrent calls safely", func() {
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

				// Start multiple goroutines calling Close concurrently
				done := make(chan bool, 10)
				for range 10 {
					go func() {
						shoutrrr.Close()
						done <- true
					}()
				}

				// Wait for all to complete
				for range 10 {
					gomega.Eventually(done).Should(gomega.Receive())
				}

				// Should not panic and all should complete
			})
		})
	})
})

func TestSlowNotificationNotSent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter()
		if err != nil {
			t.Fatal(err)
		}

		// Wait for all goroutines to be blocked
		synctest.Wait()

		// The notification should not be sent because the router is blocked
		select {
		case <-blockingRouter.sent:
			t.Fatal("expected notification not to be sent")
		default:
			// Good, channel is empty
		}

		// Cancel to clean up goroutines
		shoutrrr.cancel()
		// Wait for sendNotifications to exit
		<-shoutrrr.done
	})
}

func TestSlowNotificationSent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter()
		if err != nil {
			t.Fatal(err)
		}

		// Unlock the router
		blockingRouter.unlock <- true

		// Wait for the notification to be sent
		synctest.Wait()

		// The notification should be sent
		select {
		case sent := <-blockingRouter.sent:
			if !sent {
				t.Fatal("expected notification to be sent")
			}
		default:
			t.Fatal("expected notification to be sent")
		}

		// Cancel to clean up
		shoutrrr.cancel()
		// Wait for goroutine to exit
		<-shoutrrr.done
	})
}

func TestGracefulTerminationNotificationGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Set up logging like Ginkgo does
		logBuffer := gbytes.NewBuffer()
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.TraceLevel)
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors:    true,
			DisableTimestamp: true,
		})

		shoutrrr := createNotifier(
			[]string{"logger://"},
			allButTrace,
			"",
			true,
			StaticData{},
			true, // stdout
			time.Duration(0),
		)

		// Start the notification goroutine manually
		go sendNotifications(shoutrrr)

		// Cancel the context directly while goroutine is waiting in select
		shoutrrr.cancel()

		// Wait for the goroutine to finish (done channel should be signaled)
		synctest.Wait()

		// Verify done channel received
		select {
		case <-shoutrrr.done:
			// Good
		default:
			t.Fatal("expected done channel to be signaled")
		}
	})
}

func TestGracefulTerminationDuringMessageProcessing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Set up logging
		logBuffer := gbytes.NewBuffer()
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.TraceLevel)
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors:    true,
			DisableTimestamp: true,
		})

		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter()
		if err != nil {
			t.Fatal(err)
		}

		// Unlock the router to allow the message processing to complete
		blockingRouter.unlock <- true

		// Wait for the notification to be sent
		synctest.Wait()

		// Verify that the message was sent
		select {
		case sent := <-blockingRouter.sent:
			if !sent {
				t.Fatal("expected message to be sent")
			}
		default:
			t.Fatal("expected message to be sent")
		}

		// Cancel context to test graceful termination
		shoutrrr.cancel()

		// Wait for goroutine to finish
		synctest.Wait()

		// Verify done channel signaled
		select {
		case <-shoutrrr.done:
			// Good
		default:
			t.Fatal("expected done channel to be signaled")
		}
	})
}

func TestContextCancellationIndependentOfStopChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Set up logging
		logBuffer := gbytes.NewBuffer()
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.TraceLevel)
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors:    true,
			DisableTimestamp: true,
		})

		shoutrrr := createNotifier(
			[]string{"logger://"},
			allButTrace,
			"",
			true,
			StaticData{},
			true, // stdout
			time.Duration(0),
		)

		// Start the notification goroutine manually
		go sendNotifications(shoutrrr)

		// Test that context cancellation works without closing stop channel
		shoutrrr.cancel()

		// Wait for goroutine to finish via done channel
		synctest.Wait()

		// Verify done channel received
		select {
		case <-shoutrrr.done:
			// Good
		default:
			t.Fatal("expected done channel to be signaled")
		}

		// Verify stop channel is still open (not closed by context cancellation)
		select {
		case <-shoutrrr.stop:
			t.Fatal("stop channel should not be closed by context cancellation")
		default:
			// Good, stop channel is still open
		}
	})
}

// mockRouter implements the router interface for testing error scenarios.
type mockRouter struct {
	sendErrors []error
}

func (m *mockRouter) Send(_ string, _ *types.Params) []error {
	return m.sendErrors
}

// blockingRouter simulates a notification router with blocking behavior for testing.
// It waits for an unlock signal before sending and signals completion via a channel.
type blockingRouter struct {
	unlock chan bool
	sent   chan bool
	ctx    context.Context //nolint:containedctx
}

func (b blockingRouter) Send(_ string, _ *types.Params) []error {
	select {
	case <-b.unlock:
		b.sent <- true
	case <-b.ctx.Done():
		// Cancelled, don't send
	}

	return nil
}

// sendNotificationsWithBlockingRouter creates a notifier with a blocking router for testing.
// It queues a message and returns the notifier and router to verify notification delays.
func sendNotificationsWithBlockingRouter() (*shoutrrrTypeNotifier, *blockingRouter, error) {
	legacy := true
	ctx, cancel := context.WithCancel(context.Background())

	router := &blockingRouter{
		unlock: make(chan bool, 1),
		sent:   make(chan bool, 1),
		ctx:    ctx,
	}

	tpl, err := getShoutrrrTemplate("", legacy)
	if err != nil {
		cancel()

		return nil, nil, err
	}

	shoutrrr := &shoutrrrTypeNotifier{
		template:       tpl,
		messages:       make(chan string, 1),
		done:           make(chan struct{}),
		Router:         router,
		legacyTemplate: legacy,
		params:         &types.Params{},
		ctx:            ctx,
		cancel:         cancel,
		delay:          time.Duration(0),
	}

	entry := &logrus.Entry{
		Message: "foo bar",
	}

	go sendNotifications(shoutrrr)

	shoutrrr.StartNotification(false)
	_ = shoutrrr.Fire(entry)
	shoutrrr.SendNotification(nil)

	return shoutrrr, router, nil
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
