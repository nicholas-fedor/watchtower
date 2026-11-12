package notify

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// TestNotifier verifies the notifier that NewNotifier builds from the
// notification settings: its services, title, delay, and the shoutrrr URLs of
// the legacy notifiers. It also verifies that NewNotifier returns an error for
// a legacy notifier that cannot be converted.
func TestNotifier(t *testing.T) {
	t.Parallel()

	t.Run("the notifier", func(t *testing.T) {
		t.Parallel()

		t.Run("only empty notifier types are provided", func(t *testing.T) {
			t.Parallel()

			t.Run("should configure no services", func(t *testing.T) {
				t.Parallel()

				notifier, err := NewNotifier(newTestLogger(t), loadNotify(t, []string{
					"--notifications",
					"shoutrrr",
				}))
				require.NoError(t, err)

				assert.Empty(t, notifier.GetNames())
			})
		})
		t.Run("title is overridden in flag", func(t *testing.T) {
			t.Parallel()

			t.Run("should use the specified hostname in the title", func(t *testing.T) {
				t.Parallel()

				data := templateDataFromFlags(t, []string{
					"--notifications-hostname",
					"test.host",
				})
				title := data.Title
				assert.Equal(t, "Watchtower updates on test.host", title)
			})
		})
		t.Run("no hostname can be resolved", func(t *testing.T) {
			t.Parallel()

			t.Run("should use the default simple title", func(t *testing.T) {
				t.Parallel()

				title := getTitle(newTestLogger(t), "", "")
				assert.Equal(t, "Watchtower updates", title)
			})
		})
		t.Run("title tag is set", func(t *testing.T) {
			t.Parallel()

			t.Run("should use the prefix in the title", func(t *testing.T) {
				t.Parallel()

				data := templateDataFromFlags(t, []string{
					"--notification-title-tag",
					"PREFIX",
				})
				assert.True(t, strings.HasPrefix(data.Title, "[PREFIX]"))
			})
		})
		t.Run("legacy email tag is set", func(t *testing.T) {
			t.Parallel()

			//nolint:godox
			// TODO: Remove legacy email subjecttag test when legacy notification types are removed.
			t.Run("should use the prefix in the title", func(t *testing.T) {
				t.Parallel()

				data := templateDataFromFlags(t, []string{
					"--notification-email-subjecttag",
					"PREFIX",
				})
				assert.True(t, strings.HasPrefix(data.Title, "[PREFIX]"))
			})
		})
		t.Run("the skip title flag is set", func(t *testing.T) {
			t.Parallel()

			t.Run("should return an empty title", func(t *testing.T) {
				t.Parallel()

				data := templateDataFromFlags(t, []string{
					"--notification-skip-title",
				})
				assert.Empty(t, data.Title)
			})
		})
		t.Run("notification template file is specified", func(t *testing.T) {
			t.Parallel()

			t.Run("should load template from file", func(t *testing.T) {
				t.Parallel()

				content := "{{.Data.Host}} updated"
				tmpFile, err := os.CreateTemp("", "template")
				require.NoError(t, err)

				defer os.Remove(tmpFile.Name())

				_, err = tmpFile.WriteString(content)
				require.NoError(t, err)
				tmpFile.Close()

				notifier, err := NewNotifier(newTestLogger(t), loadNotify(t, []string{
					"--notification-url",
					"logger://",
					"--notification-template-file",
					tmpFile.Name(),
				}))
				require.NoError(t, err)
				require.NotNil(t, notifier)
				assert.Contains(t, notifier.GetNames(), "logger")
			})
		})
	})
	//nolint:godox
	// TODO: Remove legacy slack notifier tests when legacy notification types are removed.
	t.Run("the slack notifier", func(t *testing.T) {
		t.Parallel()

		t.Run("passing a discord url to the slack notifier", func(t *testing.T) {
			t.Parallel()

			channel := "123456789"
			token := "abvsihdbau"
			color := legacy.ColorInt
			username := "containrrrbot"
			iconURL := "https://github.com/nicholas-fedor/watchtower/blob/main/watchtower-sq180.png"
			expected := fmt.Sprintf(
				"discord://%s@%s?color=0x%x&colordebug=0x0&colorerror=0x0&colorinfo=0x0&colorwarn=0x0&username=watchtower",
				token,
				channel,
				color,
			)
			buildArgs := func(url string) []string {
				return []string{
					"--notifications",
					"slack",
					"--notification-slack-hook-url",
					url,
				}
			}

			t.Run("should return a discord url when using a hook url with the domain discord.com", func(t *testing.T) {
				t.Parallel()

				hookURL := fmt.Sprintf(
					"https://%s/api/webhooks/%s/%s/slack",
					"discord.com",
					channel,
					token,
				)
				testURL(t, buildArgs(hookURL), expected, time.Duration(0))
			})
			t.Run("should return a discord url when using a hook url with the domain discordapp.com", func(t *testing.T) {
				t.Parallel()

				hookURL := fmt.Sprintf(
					"https://%s/api/webhooks/%s/%s/slack",
					"discordapp.com",
					channel,
					token,
				)
				testURL(t, buildArgs(hookURL), expected, time.Duration(0))
			})
			t.Run("icon URL and username are specified", func(t *testing.T) {
				t.Parallel()

				t.Run("should return the expected URL", func(t *testing.T) {
					t.Parallel()

					hookURL := fmt.Sprintf(
						"https://%s/api/webhooks/%s/%s/slack",
						"discord.com",
						channel,
						token,
					)
					expectedOutput := fmt.Sprintf(
						"discord://%s@%s?avatar=%s&color=0x%x&colordebug=0x0&colorerror=0x0&colorinfo=0x0&colorwarn=0x0&username=%s",
						token,
						channel,
						url.QueryEscape(iconURL),
						color,
						username,
					)
					expectedDelay := 7 * time.Second
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

					testURL(t, args, expectedOutput, expectedDelay)
				})
			})
		})
		t.Run("converting a slack service config into a shoutrrr url", func(t *testing.T) {
			t.Parallel()

			username := "containrrrbot"
			tokenA := "AAAAAAAAA"
			tokenB := "BBBBBBBBB"
			tokenC := "123456789123456789123456"
			color := url.QueryEscape(legacy.ColorHex)
			iconURL := "https://github.com/nicholas-fedor/watchtower/blob/main/watchtower-sq180.png"
			iconEmoji := "whale"

			t.Run("icon URL is specified", func(t *testing.T) {
				t.Parallel()

				t.Run("should return the expected URL", func(t *testing.T) {
					t.Parallel()

					hookURL := fmt.Sprintf(
						"https://hooks.slack.com/services/%s/%s/%s",
						tokenA,
						tokenB,
						tokenC,
					)
					expectedOutput := fmt.Sprintf(
						"slack://hook:%s-%s-%s@webhook?botname=%s&color=%s&icon=%s",
						tokenA,
						tokenB,
						tokenC,
						username,
						color,
						url.QueryEscape(iconURL),
					)
					expectedDelay := 7 * time.Second

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

					testURL(t, args, expectedOutput, expectedDelay)
				})
			})

			t.Run("icon emoji is specified", func(t *testing.T) {
				t.Parallel()

				t.Run("should return the expected URL", func(t *testing.T) {
					t.Parallel()

					hookURL := fmt.Sprintf(
						"https://hooks.slack.com/services/%s/%s/%s",
						tokenA,
						tokenB,
						tokenC,
					)
					expectedOutput := fmt.Sprintf(
						"slack://hook:%s-%s-%s@webhook?botname=%s&color=%s&icon=%s",
						tokenA,
						tokenB,
						tokenC,
						username,
						color,
						iconEmoji,
					)

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

					testURL(t, args, expectedOutput, time.Duration(0))
				})
			})
		})
		t.Run("the hook URL is empty", func(t *testing.T) {
			t.Parallel()

			t.Run("should return an error with a clear missing argument message", func(t *testing.T) {
				t.Parallel()

				expectNewNotifierError(t, []string{
					"--notifications",
					"slack",
				}, `create "slack" notification config: the notification-slack-hook-url configuration option is empty`)
			})
		})
	})

	//nolint:godox
	// TODO: Remove legacy gotify notifier tests when legacy notification types are removed.
	t.Run("the gotify notifier", func(t *testing.T) {
		t.Parallel()

		t.Run("converting a gotify service config into a shoutrrr url", func(t *testing.T) {
			t.Parallel()

			t.Run("should return the expected URL", func(t *testing.T) {
				t.Parallel()

				token := "Aaaaaaaaaaaaaaa"
				host := "shoutrrr.local"

				expectedOutput := fmt.Sprintf("gotify://%s/%s?title=", host, token)

				args := []string{
					"--notifications",
					"gotify",
					"--notification-gotify-url",
					"https://" + host,
					"--notification-gotify-token",
					token,
				}

				testURL(t, args, expectedOutput, time.Duration(0))
			})
		})
		t.Run("initializing a gotify notifier via NewNotifier", func(t *testing.T) {
			t.Parallel()

			t.Run("should configure with valid flags", func(t *testing.T) {
				t.Parallel()

				args := []string{
					"--notifications",
					"gotify",
					"--notification-gotify-url",
					"https://gotify.example.com",
					"--notification-gotify-token",
					"Atesttoken12345",
					"--notification-gotify-tls-skip-verify",
				}
				notifier, err := NewNotifier(newTestLogger(t), loadNotify(t, args))
				require.NoError(t, err)

				names := notifier.GetNames()
				assert.Contains(t, names, "gotify")

				urls := notifier.urls
				assert.True(
					t,
					slices.ContainsFunc(urls, func(u string) bool { return strings.Contains(u, "gotify://gotify.example.com/Atesttoken12345") }),
					"no URL contains %q: %v",
					"gotify://gotify.example.com/Atesttoken12345",
					urls,
				)
			})

			t.Run("should leave the token out of the trace-level log", func(t *testing.T) {
				t.Parallel()

				args := []string{
					"--notifications",
					"gotify",
					"--notification-gotify-url",
					"https://gotify.example.com",
					"--notification-gotify-token",
					"Atesttoken12345",
				}

				log, buf := logcapture.NewJSON(zerolog.TraceLevel)

				notifier, err := NewNotifier(log, loadNotify(t, args))
				require.NoError(t, err)
				assert.Contains(t, notifier.GetNames(), "gotify")

				out := buf.String()
				assert.Contains(t, logcapture.Messages(t, out, "trace"), "Created Shoutrrr URL from legacy notifier")
				assert.NotContains(t, out, "Atesttoken12345")
			})
		})
	})

	//nolint:godox
	// TODO: Remove legacy msteams notifier tests when legacy notification types are removed.
	t.Run("the teams notifier", func(t *testing.T) {
		t.Parallel()

		t.Run("converting a teams service config into a shoutrrr url", func(t *testing.T) {
			t.Parallel()

			t.Run("should return the expected URL", func(t *testing.T) {
				t.Parallel()

				color := url.QueryEscape(legacy.ColorHex)

				// Power Automate workflow incoming webhook URL.
				hookURL := "https://default.environment.api.powerplatform.com/powerautomate/automations/direct/workflows/abc123/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=XXXXXXXX"
				expectedOutput := fmt.Sprintf(
					"teams:?color=%s&host=%s",
					color,
					url.QueryEscape(hookURL),
				)

				args := []string{
					"--notifications",
					"msteams",
					"--notification-msteams-hook",
					hookURL,
				}

				testURL(t, args, expectedOutput, time.Duration(0))
			})
		})
	})

	//nolint:godox
	// TODO: Remove legacy email notifier tests when legacy notification types are removed.
	t.Run("the email notifier", func(t *testing.T) {
		t.Parallel()

		t.Run("converting an email service config into a shoutrrr url", func(t *testing.T) {
			t.Parallel()

			t.Run("should set the from address in the URL", func(t *testing.T) {
				t.Parallel()

				fromAddress := "lala@example.com"
				expectedOutput := buildExpectedURL(
					"containrrrbot",
					"secret-password",
					"mail.watchtower.dev",
					25,
					fromAddress,
					"mail@example.com",
					"Plain",
				)
				expectedDelay := 7 * time.Second

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
					"mail.watchtower.dev",
					"--notifications-delay",
					fmt.Sprint(expectedDelay.Seconds()),
				}
				testURL(t, args, expectedOutput, expectedDelay)
			})

			t.Run("should return the expected URL", func(t *testing.T) {
				t.Parallel()

				fromAddress := "sender@example.com"
				toAddress := "receiver@example.com"
				expectedOutput := buildExpectedURL(
					"containrrrbot",
					"secret-password",
					"mail.watchtower.dev",
					25,
					fromAddress,
					toAddress,
					"Plain",
				)
				expectedDelay := 7 * time.Second

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
					"mail.watchtower.dev",
					"--notification-email-delay",
					fmt.Sprint(expectedDelay.Seconds()),
				}

				testURL(t, args, expectedOutput, expectedDelay)
			})
		})
		t.Run("a required field is empty", func(t *testing.T) {
			t.Parallel()

			t.Run("should return an error when the from address is empty", func(t *testing.T) {
				t.Parallel()

				expectNewNotifierError(t, []string{
					"--notifications",
					"email",
					"--notification-email-to",
					"to@example.com",
					"--notification-email-server",
					"mail.example.com",
				}, `create "email" notification config: the notification-email-from configuration option is empty`)
			})

			t.Run("should return an error when the to address is empty", func(t *testing.T) {
				t.Parallel()

				expectNewNotifierError(t, []string{
					"--notifications",
					"email",
					"--notification-email-from",
					"from@example.com",
					"--notification-email-server",
					"mail.example.com",
				}, `create "email" notification config: the notification-email-to configuration option is empty`)
			})

			t.Run("should return an error when the server is empty", func(t *testing.T) {
				t.Parallel()

				expectNewNotifierError(t, []string{
					"--notifications",
					"email",
					"--notification-email-from",
					"from@example.com",
					"--notification-email-to",
					"to@example.com",
				}, `create "email" notification config: the notification-email-server configuration option is empty`)
			})
		})
	})

	t.Run("a legacy notification type cannot be converted", func(t *testing.T) {
		t.Parallel()

		t.Run("should return an error for an unknown type", func(t *testing.T) {
			t.Parallel()

			expectNewNotifierError(t, []string{
				"--notifications",
				"carrier-pigeon",
			}, `unknown notification type: "carrier-pigeon"`)
		})

		t.Run("should return an error when the notifier cannot build its URL", func(t *testing.T) {
			t.Parallel()

			expectNewNotifierError(t, []string{
				"--notifications",
				"slack",
				"--notification-slack-hook-url",
				"https://discord.com/api/webhooks/123/abc",
			}, `create "slack" notification config: invalid Discord webhook URL, expected https://discord.com/api/webhooks/<id>/<token>/slack`)
		})
	})
}

// TestNewNotifier_SetupErrors verifies that NewNotifier returns an error
// instead of a notifier when the notifications level is invalid or the
// template file cannot be read.
func TestNewNotifier_SetupErrors(t *testing.T) {
	t.Parallel()

	missingTemplate := filepath.Join(t.TempDir(), "missing.tpl")

	tests := []struct {
		name    string
		cfg     notifyConfig.Notify
		wantErr string
	}{
		{
			name:    "invalid notifications level",
			cfg:     notifyConfig.Notify{Level: "loud"},
			wantErr: `parse notifications level: unknown log level: "loud"`,
		},
		{
			name:    "unreadable template file",
			cfg:     notifyConfig.Notify{Level: "info", TemplateFile: missingTemplate},
			wantErr: "read notification template file: open " + missingTemplate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			notifier, err := NewNotifier(newTestLogger(t), tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, notifier)
		})
	}
}

// TestRejectsURLsThatShoutrrrRejects verifies that BuildURLs and NewNotifier
// fail with the same error for a configured or converted URL that Shoutrrr
// rejects when it reads it. The error names the service and never contains the
// URL. A configured URL is named by its position, counting from 1. A converted
// URL is named by its legacy notification type.
func TestRejectsURLsThatShoutrrrRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  notifyConfig.Notify
		// source is the part of the error that names the URL and its service.
		source string
	}{
		{
			name:   "configured Discord URL with a non-numeric webhook ID",
			cfg:    notifyConfig.Notify{URLs: []string{"logger://", "discord://secrettoken@notanumber"}},
			source: "notification URL 2: discord:",
		},
		{
			name:   "configured URL with an invalid escape",
			cfg:    notifyConfig.Notify{URLs: []string{"discord://secrettoken@123456/%zz"}},
			source: "notification URL 1: failed to parse URL",
		},
		{
			name:   "configured URL with an invalid port",
			cfg:    notifyConfig.Notify{URLs: []string{"discord://123456:secrettoken/hook"}},
			source: "notification URL 1: failed to parse URL",
		},
		{
			name: "legacy email server with an invalid port",
			cfg: notifyConfig.Notify{
				LegacyTypes: []string{"email"},
				Legacy: notifyConfig.Legacy{
					EmailFrom:   "watchtower@example.com",
					EmailTo:     "admin@example.com",
					EmailServer: "smtp.example.com:secrettoken",
					EmailPort:   25,
				},
			},
			source: `check "email" notification URL: failed to parse URL`,
		},
		{
			name:   "configured docker-secret file path instead of a URL",
			cfg:    notifyConfig.Notify{URLs: []string{"logger://", "docker-secret:/run/secrets/secrettoken"}},
			source: "notification URL 2:",
		},
		{
			name: "legacy Discord wrapper with a non-numeric webhook ID",
			cfg: notifyConfig.Notify{
				LegacyTypes: []string{"slack"},
				Legacy: notifyConfig.Legacy{
					SlackHookURL: "https://discord.com/api/webhooks/notanumber/secrettoken/slack",
				},
			},
			source: `check "slack" notification URL: discord:`,
		},
		{
			name: "legacy Discord wrapper rejected after a valid Gotify type",
			cfg: notifyConfig.Notify{
				LegacyTypes: []string{"gotify", "slack"},
				Legacy: notifyConfig.Legacy{
					GotifyURL:    "https://gotify.example.com",
					GotifyToken:  "Aaaaaaaaaaaaaaa",
					SlackHookURL: "https://discord.com/api/webhooks/notanumber/secrettoken/slack",
				},
			},
			source: `check "slack" notification URL: discord:`,
		},
		{
			name: "legacy Teams webhook URL that does not parse",
			cfg: notifyConfig.Notify{
				LegacyTypes: []string{"msteams"},
				Legacy: notifyConfig.Legacy{
					MSTeamsHook: "https://default.environment.api.powerplatform.com/powerautomate/automations/direct" +
						"/workflows/secrettoken%zz/triggers/manual/paths/invoke?sig=abc",
				},
			},
			source: `check "msteams" notification URL: teams: invalid webhook URL format: the URL does not parse`,
		},
		{
			name: "legacy Teams retired connector URL",
			cfg: notifyConfig.Notify{
				LegacyTypes: []string{"msteams"},
				Legacy: notifyConfig.Legacy{
					MSTeamsHook: "https://example.webhook.office.com/webhookb2/secrettoken",
				},
			},
			source: `check "msteams" notification URL: teams: invalid webhook URL format: not a Power Automate workflow webhook URL`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			t.Run("BuildURLs", func(t *testing.T) {
				t.Parallel()

				urls, err := BuildURLs(nil, tt.cfg)
				require.ErrorContains(t, err, tt.source)
				assert.NotContains(t, err.Error(), "secrettoken")
				assert.Nil(t, urls)
			})

			t.Run("NewNotifier", func(t *testing.T) {
				t.Parallel()

				cfg := tt.cfg
				cfg.Level = "info"

				notifier, err := NewNotifier(newTestLogger(t), cfg)
				require.ErrorContains(t, err, tt.source)
				assert.NotContains(t, err.Error(), "secrettoken")
				assert.Nil(t, notifier)
			})
		})
	}
}

// TestNewSender_AcceptsGotifyURLFromFileExpansion verifies that newSender
// accepts a valid Gotify URL matching expanded secret content.
func TestNewSender_AcceptsGotifyURLFromFileExpansion(t *testing.T) {
	t.Parallel()

	cfg := notifyConfig.Notify{URLs: []string{"gotify://gotify.example.com/Atoken123456789"}}

	sender, urls, err := newSender(newTestLogger(t), cfg)
	require.NoError(t, err)
	assert.NotNil(t, sender)
	assert.Equal(t, cfg.URLs, urls)
}

// TestNewSender_LogsShoutrrrRecordsAtTheirLevel verifies that newSender gives
// Shoutrrr a loop-safe logger. A Shoutrrr warning is a warn entry that is not
// notified. The logger service's text is still logged without a level. Both
// messages start with the Shoutrrr prefix.
func TestNewSender_LogsShoutrrrRecordsAtTheirLevel(t *testing.T) {
	t.Parallel()

	// entry holds the fields of a log entry that the sender's logger decides.
	type entry struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Notify  string `json:"notify"`
	}

	log, buf := logcapture.NewJSON(zerolog.InfoLevel)

	sender, _, err := newSender(log, notifyConfig.Notify{URLs: []string{"logger://", newGenericWebhookURL(t)}})
	require.NoError(t, err)

	for _, err := range sender.Send("Found new image", &shoutrrrTypes.Params{}) {
		require.NoError(t, err)
	}

	assert.ElementsMatch(t, []entry{
		{Level: "", Message: "Shoutrrr: Found new image", Notify: "no"},
		{Level: "warn", Message: "Shoutrrr: sending without TLS", Notify: "no"},
	}, logcapture.Entries[entry](t, buf.String()))
}

// TestNewSender_PrintsLoggerTextToStdout verifies that with LogStdout set, the
// logger service's text goes to standard output without the Shoutrrr prefix.
// Only Shoutrrr's own records reach the Watchtower log.
//
// It replaces os.Stdout. It does not run in parallel for that reason.
func TestNewSender_PrintsLoggerTextToStdout(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	stdout := os.Stdout
	os.Stdout = writer

	t.Cleanup(func() { os.Stdout = stdout })

	log, buf := logcapture.NewJSON(zerolog.InfoLevel)

	sender, _, err := newSender(log, notifyConfig.Notify{URLs: []string{"logger://", newGenericWebhookURL(t)}, LogStdout: true})
	require.NoError(t, err)

	for _, err := range sender.Send("Found new image", &shoutrrrTypes.Params{}) {
		require.NoError(t, err)
	}

	os.Stdout = stdout

	require.NoError(t, writer.Close())

	printed, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	assert.Equal(t, "Found new image\n", string(printed))

	logs := buf.String()
	assert.Equal(t, []string{"Shoutrrr: sending without TLS"}, logcapture.Messages(t, logs, "warn"))
	assert.NotContains(t, logs, "Found new image")
}

// TestBuildURLs_LogsShoutrrrWarnings verifies that the warnings Shoutrrr logs
// while it reads a URL, such as one for a deprecated key, reach BuildURLs' logger.
func TestBuildURLs_LogsShoutrrrWarnings(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := zerolog.New(&buf)
	cfg := notifyConfig.Notify{
		URLs: []string{"gotify://gotify.example.com/Agotifytoken123?insecureskipverify=yes"},
	}

	urls, err := BuildURLs(&log, cfg)
	require.NoError(t, err)
	assert.Equal(t, cfg.URLs, urls)
	assert.Contains(t, buf.String(), `"level":"warn"`)
	assert.Contains(t, buf.String(), "insecureskipverify")
}

// TestBuildURLs_ConversionErrors verifies that BuildURLs returns an error that
// names the legacy type when a type cannot be converted. That covers an
// unknown type and a missing required setting. It also covers a legacy
// notifier that cannot generate its URL.
func TestBuildURLs_ConversionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     notifyConfig.Notify
		wantErr string
	}{
		{
			name:    "unknown type",
			cfg:     notifyConfig.Notify{LegacyTypes: []string{"carrier-pigeon"}},
			wantErr: `unknown notification type: "carrier-pigeon"`,
		},
		{
			name: "notifier cannot generate its URL",
			cfg: notifyConfig.Notify{
				LegacyTypes: []string{"slack"},
				Legacy:      notifyConfig.Legacy{SlackHookURL: "https://discord.com/api/webhooks/123/abc"},
			},
			wantErr: `create "slack" notification config: invalid Discord webhook URL`,
		},
		{
			name:    "missing required setting",
			cfg:     notifyConfig.Notify{LegacyTypes: []string{"slack"}},
			wantErr: `create "slack" notification config: the notification-slack-hook-url configuration option is empty`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			urls, err := BuildURLs(nil, tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, urls)
		})
	}
}

// TestBuildURLs_ConvertsEveryLegacyType verifies that BuildURLs converts every
// legacy type, not only the first one. It returns the configured URLs followed
// by the converted URLs in type order.
func TestBuildURLs_ConvertsEveryLegacyType(t *testing.T) {
	t.Parallel()

	cfg := notifyConfig.Notify{
		URLs:        []string{"logger://"},
		LegacyTypes: []string{"slack", "gotify"},
		Legacy: notifyConfig.Legacy{
			SlackHookURL: "https://discord.com/api/webhooks/123456789/abc/slack",
			GotifyURL:    "https://gotify.example.com",
			GotifyToken:  "Aaaaaaaaaaaaaaa",
		},
	}

	urls, err := BuildURLs(nil, cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"logger://",
		fmt.Sprintf(
			"discord://abc@123456789?color=0x%x&colordebug=0x0&colorerror=0x0&colorinfo=0x0&colorwarn=0x0",
			legacy.ColorInt,
		),
		"gotify://gotify.example.com/Aaaaaaaaaaaaaaa?title=",
	}, urls)
}

// TestBuildURLs_StopsAtTheFirstRejectedURL verifies that BuildURLs checks each
// converted URL before it converts the next legacy type. Shoutrrr rejects the
// Slack hook's Discord URL. The email type, whose settings are all empty, would
// return its own error if it were converted first.
func TestBuildURLs_StopsAtTheFirstRejectedURL(t *testing.T) {
	t.Parallel()

	cfg := notifyConfig.Notify{
		LegacyTypes: []string{"slack", "email"},
		Legacy: notifyConfig.Legacy{
			SlackHookURL: "https://discord.com/api/webhooks/notanumber/secrettoken/slack",
		},
	}

	urls, err := BuildURLs(nil, cfg)
	require.ErrorContains(t, err, `check "slack" notification URL`)
	assert.Nil(t, urls)
}

// expectNewNotifierError builds a notifier from args and requires NewNotifier
// to return the given error and no notifier.
//
// Parameters:
//   - t: Test that receives the failures.
//   - args: CLI flags to load before constructing the notifier.
//   - wantErr: Exact error text NewNotifier must return.
func expectNewNotifierError(t *testing.T, args []string, wantErr string) {
	t.Helper()

	notifier, err := NewNotifier(newTestLogger(t), loadNotify(t, args))
	require.EqualError(t, err, wantErr)
	assert.Nil(t, notifier)
}

// TODO: Remove buildExpectedURL helper when legacy notification tests are removed.
//
//nolint:godox
func buildExpectedURL(
	username string,
	password string,
	host string,
	port int,
	from string,
	destAddress string,
	auth string,
) string {
	template := "smtp://%s:%s@%s:%d/?auth=%s&clienthost=localhost&encryption=Auto&fromaddress=%s&fromname=Watchtower&subject=&toaddresses=%s&usehtml=No&usestarttls=Yes&timeout=10s"

	return fmt.Sprintf(template,
		url.QueryEscape(username),
		url.QueryEscape(password),
		host, port, auth,
		url.QueryEscape(from),
		url.QueryEscape(destAddress))
}

// TestGetDelay verifies which delay the notifier waits before each send. The
// legacy email delay applies only when the email notification type is
// configured. It then wins over the notifications delay.
func TestGetDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  notifyConfig.Notify
		want time.Duration
	}{
		{name: "no delay is defined", cfg: notifyConfig.Notify{}, want: 0},
		{name: "notifications delay", cfg: notifyConfig.Notify{DelaySeconds: 5}, want: 5 * time.Second},
		{name: "negative notifications delay", cfg: notifyConfig.Notify{DelaySeconds: -5}, want: 0},
		{
			name: "legacy email delay",
			cfg:  notifyConfig.Notify{LegacyTypes: []string{"email"}, Legacy: notifyConfig.Legacy{EmailDelay: 5}},
			want: 5 * time.Second,
		},
		{
			name: "legacy email delay wins over the notifications delay",
			cfg: notifyConfig.Notify{
				DelaySeconds: 5,
				LegacyTypes:  []string{"email"},
				Legacy:       notifyConfig.Legacy{EmailDelay: 7},
			},
			want: 7 * time.Second,
		},
		{
			name: "negative legacy email delay falls back to the notifications delay",
			cfg: notifyConfig.Notify{
				DelaySeconds: 5,
				LegacyTypes:  []string{"email"},
				Legacy:       notifyConfig.Legacy{EmailDelay: -7},
			},
			want: 5 * time.Second,
		},
		{
			name: "legacy email delay without the email type",
			cfg: notifyConfig.Notify{
				DelaySeconds: 5,
				LegacyTypes:  []string{"slack"},
				Legacy:       notifyConfig.Legacy{EmailDelay: 7},
			},
			want: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, getDelay(tt.cfg))
		})
	}
}

// TestLogLegacyDeprecationWarnings verifies that each legacy notification type
// gets one deprecation warning, in the order given. The shoutrrr type and an
// unknown type get none.
func TestLogLegacyDeprecationWarnings(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.InfoLevel)

	LogLegacyDeprecationWarnings(log, []string{"shoutrrr", "email", "carrier-pigeon", "slack", "msteams", "gotify"})

	assert.Equal(t, []string{
		"Using deprecated legacy email notification configuration. Use the notification-url configuration option instead.",
		"Using deprecated legacy slack notification configuration. Use the notification-url configuration option instead.",
		"Using deprecated legacy msteams notification configuration. Use the notification-url configuration option instead.",
		"Using deprecated legacy gotify notification configuration. Use the notification-url configuration option instead.",
	}, logcapture.Messages(t, buf.String(), "warn"))
}

// TODO: Remove testURL helper when legacy notification tests are removed.
//
//nolint:godox
func testURL(t *testing.T, args []string, expectedURL string, expectedDelay time.Duration) {
	t.Helper()

	cfg := loadNotify(t, args)

	notifier, err := NewNotifier(newTestLogger(t), cfg)
	require.NoError(t, err)
	t.Cleanup(notifier.Close)

	assert.Contains(t, notifier.urls, expectedURL)
	assert.Equal(t, expectedDelay, notifier.delay)
}

// loadNotify loads the notification settings from args the way production
// does, by parsing them into a command with every flag registered and running
// settings.Load.
//
// Parameters:
//   - t: Test that receives the failures.
//   - args: CLI flags to parse.
//
// Returns:
//   - notifyConfig.Notify: The resolved notification settings.
func loadNotify(t *testing.T, args []string) notifyConfig.Notify {
	t.Helper()

	command := &cobra.Command{Use: "watchtower"}
	settings.RegisterAll(command)
	require.NoError(t, command.ParseFlags(args))

	cfg, err := settings.Load(newTestLogger(t), command, nil)
	require.NoError(t, err)

	return cfg.Notify
}

// templateDataFromFlags returns the static notification data for the settings loaded
// from args.
//
// Parameters:
//   - t: Test that receives the failures.
//   - args: CLI flags to parse.
//
// Returns:
//   - format.StaticData: Host and title for notification templates.
func templateDataFromFlags(t *testing.T, args []string) format.StaticData {
	t.Helper()

	cfg := loadNotify(t, args)

	return templateData(newTestLogger(t), cfg.Hostname, cfg.TitleTag, cfg.EmailSubjectTag, cfg.SkipTitle)
}
