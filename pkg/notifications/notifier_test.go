package notifications_test

import (
	"fmt"
	"net/url"
	"time"

	"github.com/nicholas-fedor/watchtower/cmd"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("notifications", func() {
	ginkgo.Describe("the notifier", func() {
		ginkgo.When("only empty notifier types are provided", func() {

			command := cmd.NewRootCommand()
			flags.RegisterNotificationFlags(command)

			err := command.ParseFlags([]string{
				"--notifications",
				"shoutrrr",
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			notifier := notifications.NewNotifier(command)

			gomega.Expect(notifier.GetNames()).To(gomega.BeEmpty())
		})
		ginkgo.When("title is overridden in flag", func() {
			ginkgo.It("should use the specified hostname in the title", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				err := command.ParseFlags([]string{
					"--notifications-hostname",
					"test.host",
				})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				data := notifications.GetTemplateData(command)
				title := data.Title
				gomega.Expect(title).To(gomega.Equal("Watchtower updates on test.host"))
			})
		})
		ginkgo.When("no hostname can be resolved", func() {
			ginkgo.It("should use the default simple title", func() {
				title := notifications.GetTitle("", "")
				gomega.Expect(title).To(gomega.Equal("Watchtower updates"))
			})
		})
		ginkgo.When("title tag is set", func() {
			ginkgo.It("should use the prefix in the title", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				gomega.Expect(command.ParseFlags([]string{
					"--notification-title-tag",
					"PREFIX",
				})).To(gomega.Succeed())

				data := notifications.GetTemplateData(command)
				gomega.Expect(data.Title).To(gomega.HavePrefix("[PREFIX]"))
			})
		})
		ginkgo.When("legacy email tag is set", func() {
			ginkgo.It("should use the prefix in the title", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				gomega.Expect(command.ParseFlags([]string{
					"--notification-email-subjecttag",
					"PREFIX",
				})).To(gomega.Succeed())

				data := notifications.GetTemplateData(command)
				gomega.Expect(data.Title).To(gomega.HavePrefix("[PREFIX]"))
			})
		})
		ginkgo.When("the skip title flag is set", func() {
			ginkgo.It("should return an empty title", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				gomega.Expect(command.ParseFlags([]string{
					"--notification-skip-title",
				})).To(gomega.Succeed())

				data := notifications.GetTemplateData(command)
				gomega.Expect(data.Title).To(gomega.BeEmpty())
			})
		})
		ginkgo.When("no delay is defined", func() {
			ginkgo.It("should use the default delay", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				delay := notifications.GetDelay(command, time.Duration(0))
				gomega.Expect(delay).To(gomega.Equal(time.Duration(0)))
			})
		})
		ginkgo.When("delay is defined", func() {
			ginkgo.It("should use the specified delay", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				err := command.ParseFlags([]string{
					"--notifications-delay",
					"5",
				})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				delay := notifications.GetDelay(command, time.Duration(0))
				gomega.Expect(delay).To(gomega.Equal(time.Duration(5) * time.Second))
			})
		})
		ginkgo.When("legacy delay is defined", func() {
			ginkgo.It("should use the specified legacy delay", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)
				delay := notifications.GetDelay(command, time.Duration(5)*time.Second)
				gomega.Expect(delay).To(gomega.Equal(time.Duration(5) * time.Second))
			})
		})
		ginkgo.When("legacy delay and delay is defined", func() {
			ginkgo.It("should use the specified legacy delay and ignore the specified delay", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				err := command.ParseFlags([]string{
					"--notifications-delay",
					"0",
				})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				delay := notifications.GetDelay(command, time.Duration(7)*time.Second)
				gomega.Expect(delay).To(gomega.Equal(time.Duration(7) * time.Second))
			})
		})
	})
	ginkgo.Describe("the slack notifier", func() {
		// builderFn := notifications.NewSlackNotifier

		ginkgo.When("passing a discord url to the slack notifier", func() {
			command := cmd.NewRootCommand()
			flags.RegisterNotificationFlags(command)

			channel := "123456789"
			token := "abvsihdbau"
			color := notifications.ColorInt
			username := "containrrrbot"
			iconURL := "https://containrrr.dev/watchtower-sq180.png"
			expected := fmt.Sprintf("discord://%s@%s?color=0x%x&colordebug=0x0&colorerror=0x0&colorinfo=0x0&colorwarn=0x0&username=watchtower", token, channel, color)
			buildArgs := func(url string) []string {
				return []string{
					"--notifications",
					"slack",
					"--notification-slack-hook-url",
					url,
				}
			}

			ginkgo.It("should return a discord url ginkgo.when using a hook url with the domain discord.com", func() {
				hookURL := fmt.Sprintf("https://%s/api/webhooks/%s/%s/slack", "discord.com", channel, token)
				testURL(buildArgs(hookURL), expected, time.Duration(0))
			})
			ginkgo.It("should return a discord url ginkgo.when using a hook url with the domain discordapp.com", func() {
				hookURL := fmt.Sprintf("https://%s/api/webhooks/%s/%s/slack", "discordapp.com", channel, token)
				testURL(buildArgs(hookURL), expected, time.Duration(0))
			})
			ginkgo.When("icon URL and username are specified", func() {
				ginkgo.It("should return the expected URL", func() {
					hookURL := fmt.Sprintf("https://%s/api/webhooks/%s/%s/slack", "discord.com", channel, token)
					expectedOutput := fmt.Sprintf("discord://%s@%s?avatar=%s&color=0x%x&colordebug=0x0&colorerror=0x0&colorinfo=0x0&colorwarn=0x0&username=%s", token, channel, url.QueryEscape(iconURL), color, username)
					expectedDelay := time.Duration(7) * time.Second
					args := []string{
						"--notifications",
						"slack",
						"--notification-slack-hook-url",
						hookURL,
						"--notification-slack-identifier",
						username,
						"--notification-slack-icon-url",
						iconURL,
						"--notifications-delay",
						fmt.Sprint(expectedDelay.Seconds()),
					}

					testURL(args, expectedOutput, expectedDelay)
				})
			})
		})
		ginkgo.When("converting a slack service config into a shoutrrr url", func() {
			command := cmd.NewRootCommand()
			flags.RegisterNotificationFlags(command)
			username := "containrrrbot"
			tokenA := "AAAAAAAAA"
			tokenB := "BBBBBBBBB"
			tokenC := "123456789123456789123456"
			color := url.QueryEscape(notifications.ColorHex)
			iconURL := "https://containrrr.dev/watchtower-sq180.png"
			iconEmoji := "whale"

			ginkgo.When("icon URL is specified", func() {
				ginkgo.It("should return the expected URL", func() {

					hookURL := fmt.Sprintf("https://hooks.slack.com/services/%s/%s/%s", tokenA, tokenB, tokenC)
					expectedOutput := fmt.Sprintf("slack://hook:%s-%s-%s@webhook?botname=%s&color=%s&icon=%s", tokenA, tokenB, tokenC, username, color, url.QueryEscape(iconURL))
					expectedDelay := time.Duration(7) * time.Second

					args := []string{
						"--notifications",
						"slack",
						"--notification-slack-hook-url",
						hookURL,
						"--notification-slack-identifier",
						username,
						"--notification-slack-icon-url",
						iconURL,
						"--notifications-delay",
						fmt.Sprint(expectedDelay.Seconds()),
					}

					testURL(args, expectedOutput, expectedDelay)
				})
			})

			ginkgo.When("icon emoji is specified", func() {
				ginkgo.It("should return the expected URL", func() {
					hookURL := fmt.Sprintf("https://hooks.slack.com/services/%s/%s/%s", tokenA, tokenB, tokenC)
					expectedOutput := fmt.Sprintf("slack://hook:%s-%s-%s@webhook?botname=%s&color=%s&icon=%s", tokenA, tokenB, tokenC, username, color, iconEmoji)

					args := []string{
						"--notifications",
						"slack",
						"--notification-slack-hook-url",
						hookURL,
						"--notification-slack-identifier",
						username,
						"--notification-slack-icon-emoji",
						iconEmoji,
					}

					testURL(args, expectedOutput, time.Duration(0))
				})
			})
		})
	})

	ginkgo.Describe("the gotify notifier", func() {
		ginkgo.When("converting a gotify service config into a shoutrrr url", func() {
			ginkgo.It("should return the expected URL", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				token := "aaa"
				host := "shoutrrr.local"

				expectedOutput := fmt.Sprintf("gotify://%s/%s?title=", host, token)

				args := []string{
					"--notifications",
					"gotify",
					"--notification-gotify-url",
					fmt.Sprintf("https://%s", host),
					"--notification-gotify-token",
					token,
				}

				testURL(args, expectedOutput, time.Duration(0))
			})
		})
	})

	ginkgo.Describe("the teams notifier", func() {
		ginkgo.When("converting a teams service config into a shoutrrr url", func() {
			ginkgo.It("should return the expected URL", func() {
				command := cmd.NewRootCommand()
				flags.RegisterNotificationFlags(command)

				tokenA := "11111111-4444-4444-8444-cccccccccccc@22222222-4444-4444-8444-cccccccccccc"
				tokenB := "33333333012222222222333333333344"
				tokenC := "44444444-4444-4444-8444-cccccccccccc"
				color := url.QueryEscape(notifications.ColorHex)

				hookURL := fmt.Sprintf("https://outlook.office.com/webhook/%s/IncomingWebhook/%s/%s", tokenA, tokenB, tokenC)
				expectedOutput := fmt.Sprintf("teams://%s/%s/%s?color=%s", tokenA, tokenB, tokenC, color)

				args := []string{
					"--notifications",
					"msteams",
					"--notification-msteams-hook",
					hookURL,
				}

				testURL(args, expectedOutput, time.Duration(0))
			})
		})
	})

	ginkgo.Describe("the email notifier", func() {
		ginkgo.When("converting an email service config into a shoutrrr url", func() {
			ginkgo.It("should set the from address in the URL", func() {
				fromAddress := "lala@example.com"
				expectedOutput := buildExpectedURL("containrrrbot", "secret-password", "mail.containrrr.dev", 25, fromAddress, "mail@example.com", "Plain")
				expectedDelay := time.Duration(7) * time.Second

				args := []string{
					"--notifications",
					"email",
					"--notification-email-from",
					fromAddress,
					"--notification-email-to",
					"mail@example.com",
					"--notification-email-server-user",
					"containrrrbot",
					"--notification-email-server-password",
					"secret-password",
					"--notification-email-server",
					"mail.containrrr.dev",
					"--notifications-delay",
					fmt.Sprint(expectedDelay.Seconds()),
				}
				testURL(args, expectedOutput, expectedDelay)
			})

			ginkgo.It("should return the expected URL", func() {

				fromAddress := "sender@example.com"
				toAddress := "receiver@example.com"
				expectedOutput := buildExpectedURL("containrrrbot", "secret-password", "mail.containrrr.dev", 25, fromAddress, toAddress, "Plain")
				expectedDelay := time.Duration(7) * time.Second

				args := []string{
					"--notifications",
					"email",
					"--notification-email-from",
					fromAddress,
					"--notification-email-to",
					toAddress,
					"--notification-email-server-user",
					"containrrrbot",
					"--notification-email-server-password",
					"secret-password",
					"--notification-email-server",
					"mail.containrrr.dev",
					"--notification-email-delay",
					fmt.Sprint(expectedDelay.Seconds()),
				}

				testURL(args, expectedOutput, expectedDelay)
			})
		})
	})
})

func buildExpectedURL(username string, password string, host string, port int, from string, to string, auth string) string {
	var template = "smtp://%s:%s@%s:%d/?auth=%s&fromaddress=%s&fromname=Watchtower&subject=&toaddresses=%s"
	return fmt.Sprintf(template,
		url.QueryEscape(username),
		url.QueryEscape(password),
		host, port, auth,
		url.QueryEscape(from),
		url.QueryEscape(to))
}

func testURL(args []string, expectedURL string, expectedDelay time.Duration) {
	defer ginkgo.GinkgoRecover()

	command := cmd.NewRootCommand()
	flags.RegisterNotificationFlags(command)

	gomega.Expect(command.ParseFlags(args)).To(gomega.Succeed())

	urls, delay := notifications.AppendLegacyUrls([]string{}, command)

	gomega.Expect(urls).To(gomega.ContainElement(expectedURL))
	gomega.Expect(delay).To(gomega.Equal(expectedDelay))
}
