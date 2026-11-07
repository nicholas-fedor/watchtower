package format

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// TODO: Remove legacyMockData when legacy notification types are removed.
//
//nolint:godox
var legacyMockData = Data{
	Entries: []*Entry{
		{
			Level:   "info",
			Message: "foo Bar",
		},
	},
}

var mockDataMultipleEntries = Data{
	Entries: []*Entry{
		{
			Level:   "info",
			Message: "The situation is under control",
		},
		{
			Level:   "warning",
			Message: "All the smoke might be covering up some problems",
		},
		{
			Level:   "error",
			Message: "Turns out everything is on fire",
		},
	},
}

var mockDataAllFresh = Data{
	Entries: []*Entry{},
	Report:  reportfixture.Report(state.FreshState),
}

// mockDataFromStates generates mock notification data with specified container states.
// It includes legacy log entries and static data for testing purposes.
//
// TODO: Remove legacyMockData reference when legacy notification types are removed.
//
//nolint:godox
func mockDataFromStates(t *testing.T, states ...state.State) Data {
	t.Helper()

	return Data{
		Entries: legacyMockData.Entries,
		Report:  reportfixture.Report(states...),
		Title:   "Watchtower updates on Mock",
		Host:    "Mock",
	}
}

// getTemplatedResult generates a templated message for testing.
// It builds and returns the message string, expecting no errors.
//
// TODO: Remove legacy parameter when legacy notification types are removed.
//
//nolint:godox
func getTemplatedResult(t *testing.T, tplString string, legacy bool, data Data) string {
	t.Helper()

	log := newTestLogger(t)

	tpl, err := New(log, tplString, legacy)
	require.NoError(t, err)
	msg, err := tpl.Render(log, data)
	require.NoError(t, err)

	return msg
}

// TestRender_CommonTemplateName verifies that naming a built-in template formats
// messages with it.
func TestRender_CommonTemplateName(t *testing.T) {
	t.Parallel()

	t.Run("should format using that template", func(t *testing.T) {
		t.Parallel()

		expected := `
updt1 (mock/updt1:latest): Updated
`[1:]
		data := mockDataFromStates(t, state.UpdatedState)
		assert.Equal(t, expected, getTemplatedResult(t, `porcelain.v1.summary-no-log`, false, data))
	})
}

// TestRender_PorcelainJSON verifies that the porcelain JSON template renders
// valid JSON with each container's details.
func TestRender_PorcelainJSON(t *testing.T) {
	t.Parallel()

	t.Run("should produce valid JSON with container details", func(t *testing.T) {
		t.Parallel()

		data := mockDataFromStates(t, state.UpdatedState, state.FreshState, state.FailedState)
		result := getTemplatedResult(t, `porcelain.json`, false, data)

		assert.JSONEq(t, `{
			"containers": [
				{
					"name": "updt1",
					"image": "mock/updt1:latest",
					"image_id": "01d110000000",
					"latest_image_id": "d0a110000000",
					"state": "Updated",
					"update_available": true
				},
				{
					"name": "fail1",
					"image": "mock/fail1:latest",
					"image_id": "01d210000000",
					"latest_image_id": "d0a210000000",
					"state": "Failed",
					"update_available": true,
					"error": "accidentally the whole container"
				},
				{
					"name": "frsh1",
					"image": "mock/frsh1:latest",
					"image_id": "01d310000000",
					"latest_image_id": "01d310000000",
					"state": "Fresh",
					"update_available": false
				}
			]
		}`, result)
	})
}

// TestRender_LegacyTemplates verifies how the legacy templates format log
// entries, with the default template or a custom one.
//
// TODO: Remove these tests when legacy notification types are removed.
//
//nolint:godox
func TestRender_LegacyTemplates(t *testing.T) {
	t.Parallel()

	t.Run("no custom template is provided", func(t *testing.T) {
		t.Parallel()

		t.Run("should format the messages using the default template", func(t *testing.T) {
			t.Parallel()

			entries := []*Entry{
				{Message: "foo bar"},
			}

			s := getTemplatedResult(t, "", true, Data{Entries: entries})
			assert.Equal(t, "foo bar", s)
		})

		t.Run("should format image usage-check skips without dumping raw fields", func(t *testing.T) {
			t.Parallel()

			entries := []*Entry{
				{
					Message: "Failed to list containers for image usage check, skipping removal",
					Data: map[string]any{
						"error":      `Get "http://socket-proxy-write:2375/v1.55/containers/json?all=1": terminated signal received`,
						"image_id":   "769846626f2f",
						"image_name": "ghcr.io/amir20/dtop:latest",
					},
				},
			}

			s := getTemplatedResult(t, "", true, Data{Entries: entries})
			assert.Equal(t, "Skipped image cleanup: ghcr.io/amir20/dtop:latest (769846626f2f): "+
				`Get "http://socket-proxy-write:2375/v1.55/containers/json?all=1": terminated signal received`, s)
			assert.NotContains(t, s, " | ")
		})

		t.Run("should format Docker image usage budget messages without dumping raw fields", func(t *testing.T) {
			t.Parallel()

			entries := []*Entry{
				{
					Message: "Docker image usage exceeds configured maximum",
					Data: map[string]any{
						"usage":       int64(10_000),
						"max":         int64(10_000),
						"warn":        int64(8_000),
						"reclaimable": int64(2_000),
						"image_count": int64(4),
					},
				},
				{
					Message: "Docker image usage exceeds configured warning threshold",
					Data: map[string]any{
						"usage":       int64(8_000),
						"max":         int64(10_000),
						"warn":        int64(8_000),
						"reclaimable": int64(2_000),
						"image_count": int64(4),
					},
				},
				{
					Message: "Failed to query Docker image disk usage",
					Data:    map[string]any{"error": "df unavailable"},
				},
				{
					Message: "Docker image usage budget enabled",
					Data: map[string]any{
						"disk_space_max":  int64(40_000_000_000),
						"disk_space_warn": int64(32_000_000_000),
					},
				},
				{
					Message: "Docker image usage exceeds configured maximum",
					Data: map[string]any{
						"usage":       int64(0),
						"max":         int64(0),
						"reclaimable": int64(0),
						"image_count": int64(0),
					},
				},
			}

			s := getTemplatedResult(t, "", true, Data{Entries: entries})
			assert.Contains(t, s, "Docker image usage exceeds configured maximum: 10 KB of 10 KB used (2 KB reclaimable, 4 images)")
			assert.Contains(t, s, "Docker image usage exceeds configured warning threshold: 8 KB of 8 KB used (2 KB reclaimable, 4 images)")
			assert.Contains(t, s, "Failed to query Docker image disk usage: df unavailable")
			assert.Contains(t, s, "Docker image usage budget enabled: maximum 40 GB, warning at 32 GB")
			assert.Contains(t, s, "Docker image usage exceeds configured maximum: 0 B of 0 B used (0 B reclaimable, 0 images)")
			assert.NotContains(t, s, "unknown")
			assert.NotContains(t, s, " | ")
		})
	})
	t.Run("given a valid custom template", func(t *testing.T) {
		t.Parallel()

		t.Run("should format the messages using the custom template", func(t *testing.T) {
			t.Parallel()

			tplString := `{{range .}}{{.Level}}: {{.Message}}{{println}}{{end}}`
			entries := []*Entry{
				{
					Level:   "info",
					Message: "foo bar",
				},
			}

			s := getTemplatedResult(t, tplString, true, Data{Entries: entries})
			assert.Equal(t, "info: foo bar\n", s)
		})
	})

	t.Run("the default template", func(t *testing.T) {
		t.Parallel()

		t.Run("all containers are fresh", func(t *testing.T) {
			t.Parallel()

			t.Run("should return an empty string", func(t *testing.T) {
				t.Parallel()

				assert.Empty(t, getTemplatedResult(t, ``, true, mockDataAllFresh))
			})
		})
	})

	t.Run("given a template that is using ToUpper function", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the text in UPPER CASE", func(t *testing.T) {
			t.Parallel()

			tplString := `{{range .}}{{ .Message | ToUpper }}{{end}}`
			assert.Equal(t, "FOO BAR", getTemplatedResult(t, tplString, true, legacyMockData))
		})
	})

	t.Run("given a template that is using ToLower function", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the text in lower case", func(t *testing.T) {
			t.Parallel()

			tplString := `{{range .}}{{ .Message | ToLower }}{{end}}`
			assert.Equal(t, "foo bar", getTemplatedResult(t, tplString, true, legacyMockData))
		})
	})

	t.Run("given a template that is using Title function", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the text in Title Case", func(t *testing.T) {
			t.Parallel()

			tplString := `{{range .}}{{ .Message | Title }}{{end}}`
			assert.Equal(t, "Foo Bar", getTemplatedResult(t, tplString, true, legacyMockData))
		})
	})
}

// TestRender_ReportTemplates verifies how report templates format a session
// report and its log entries.
func TestRender_ReportTemplates(t *testing.T) {
	t.Parallel()

	t.Run("no custom template is provided", func(t *testing.T) {
		t.Parallel()

		t.Run("should format the messages using the default template", func(t *testing.T) {
			t.Parallel()

			expected := `4 Scanned, 2 Updated, 0 Restarted, 1 Failed, 1 Fresh, 1 Skipped
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000
- updt2 (mock/updt2:latest): 01d120000000 updated to d0a120000000
- frsh1 (mock/frsh1:latest): Fresh
- skip1 (mock/skip1:latest): Skipped: unpossible
- fail1 (mock/fail1:latest): Failed: accidentally the whole container`
			data := mockDataFromStates(t,
				state.UpdatedState,
				state.FreshState,
				state.FailedState,
				state.SkippedState,
				state.UpdatedState,
			)
			assert.Equal(t, expected, getTemplatedResult(t, ``, false, data))
		})
		t.Run("should use image IDs for container update reporting", func(t *testing.T) {
			t.Parallel()

			data := mockDataFromStates(t, state.UpdatedState)
			result := getTemplatedResult(t, ``, false, data)

			// Verify that the result contains image ID formats, not container IDs
			// Image IDs in the mock data are like "01d110000000" and "d0a110000000"
			// Container IDs are like "c79110000000"
			assert.Contains(t, result, "01d110000000")
			assert.Contains(t, result, "d0a110000000")
			assert.NotContains(t, result, "c79110000000")
		})
	})

	t.Run("using a template referencing Title", func(t *testing.T) {
		t.Parallel()

		t.Run("should contain the title in the output", func(t *testing.T) {
			t.Parallel()

			expected := `Watchtower updates on Mock`
			data := mockDataFromStates(t, state.UpdatedState)
			assert.Equal(t, expected, getTemplatedResult(t, `{{ .Title }}`, false, data))
		})
	})

	t.Run("using a template referencing Host", func(t *testing.T) {
		t.Parallel()

		t.Run("should contain the hostname in the output", func(t *testing.T) {
			t.Parallel()

			expected := `Mock`
			data := mockDataFromStates(t, state.UpdatedState)
			assert.Equal(t, expected, getTemplatedResult(t, `{{ .Host }}`, false, data))
		})
	})

	t.Run("using a template referencing Git and OCI report fields", func(t *testing.T) {
		t.Parallel()

		t.Run("should render Changelog, GitRepo, and Source", func(t *testing.T) {
			t.Parallel()

			_, report := updatedApp(t, provenance.ReportMeta{
				GitRepo:         "https://github.com/org/app.git",
				GitRef:          "v1.2.3",
				Changelog:       "https://github.com/org/app/releases",
				Source:          "https://github.com/org/app",
				ImageURL:        "https://example.com/image",
				Documentation:   "https://example.com/docs",
				CurrentVersion:  "1.2.2",
				LatestVersion:   "1.2.3",
				CurrentRevision: "abc123",
			})

			got := getTemplatedResult(
				t,
				`{{ range .Report.Updated }}{{ .Name }} {{ .Changelog }}{{ if .Source }} ({{ .Source }}){{ end }} repo={{ .GitRepo }}{{ end }}`,
				false,
				Data{Report: report},
			)
			assert.Equal(t, "app https://github.com/org/app/releases (https://github.com/org/app) repo=https://github.com/org/app.git", got)
		})
	})

	t.Run("the default template", func(t *testing.T) {
		t.Parallel()

		t.Run("all containers are fresh", func(t *testing.T) {
			t.Parallel()

			t.Run("should return the summary", func(t *testing.T) {
				t.Parallel()

				expected := `1 Scanned, 0 Updated, 0 Restarted, 0 Failed, 1 Fresh, 0 Skipped
- frsh1 (mock/frsh1:latest): Fresh`
				assert.Equal(t, expected, getTemplatedResult(t, ``, false, mockDataAllFresh))
			})
		})
		t.Run("at least one container was updated", func(t *testing.T) {
			t.Parallel()

			t.Run("should send a report", func(t *testing.T) {
				t.Parallel()

				expected := `1 Scanned, 1 Updated, 0 Restarted, 0 Failed, 0 Fresh, 0 Skipped
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000`
				data := mockDataFromStates(t, state.UpdatedState)
				assert.Equal(t, expected, getTemplatedResult(t, ``, false, data))
			})
		})
		t.Run("at least one container failed to update", func(t *testing.T) {
			t.Parallel()

			t.Run("should send a report", func(t *testing.T) {
				t.Parallel()

				expected := `1 Scanned, 0 Updated, 0 Restarted, 1 Failed, 0 Fresh, 0 Skipped
- fail1 (mock/fail1:latest): Failed: accidentally the whole container`
				data := mockDataFromStates(t, state.FailedState)
				assert.Equal(t, expected, getTemplatedResult(t, ``, false, data))
			})
		})
		t.Run("containers are restarted due to dependencies", func(t *testing.T) {
			t.Parallel()

			t.Run("should send a report with restarted containers", func(t *testing.T) {
				t.Parallel()

				expected := `2 Scanned, 1 Updated, 1 Restarted, 0 Failed, 0 Fresh, 0 Skipped
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000
- rstr1 (mock/rstr1:latest): Restarted`
				data := mockDataFromStates(t, state.UpdatedState, state.RestartedState)
				assert.Equal(t, expected, getTemplatedResult(t, ``, false, data))
			})
		})
		t.Run("mixing updated and restarted containers", func(t *testing.T) {
			t.Parallel()

			t.Run("should show different states for updated vs restarted", func(t *testing.T) {
				t.Parallel()

				expected := `3 Scanned, 2 Updated, 1 Restarted, 0 Failed, 0 Fresh, 0 Skipped
- updt1 (mock/updt1:latest): 01d110000000 updated to d0a110000000
- updt2 (mock/updt2:latest): 01d120000000 updated to d0a120000000
- rstr1 (mock/rstr1:latest): Restarted`
				data := mockDataFromStates(t,
					state.UpdatedState,
					state.RestartedState,
					state.UpdatedState,
				)
				assert.Equal(t, expected, getTemplatedResult(t, ``, false, data))
			})
		})
		t.Run("testing JSON output format", func(t *testing.T) {
			t.Parallel()

			t.Run("should include restarted containers in JSON response", func(t *testing.T) {
				t.Parallel()

				data := mockDataFromStates(t, state.UpdatedState, state.RestartedState)
				jsonResult := getTemplatedResult(t, `{{ . | ToJSON }}`, false, data)

				var result map[string]any
				require.NoError(t, json.Unmarshal([]byte(jsonResult), &result))

				report, ok := result["report"].(map[string]any)
				assert.True(t, ok)

				updated, ok := report["updated"].([]any)
				assert.True(t, ok)
				require.Len(t, updated, 1)

				restarted, ok := report["restarted"].([]any)
				assert.True(t, ok)
				require.Len(t, restarted, 1)
				assert.Contains(t, restarted[0], "state")
				assert.Equal(t, "Restarted", restarted[0].(map[string]any)["state"])
			})
		})
		t.Run("the report is nil", func(t *testing.T) {
			t.Parallel()

			t.Run("should return the logged entries", func(t *testing.T) {
				t.Parallel()

				expected := `The situation is under control
All the smoke might be covering up some problems
Turns out everything is on fire
`
				assert.Equal(t, expected, getTemplatedResult(t, ``, false, mockDataMultipleEntries))
			})
		})
	})
}

// TestDefaultLegacyGitMessages verifies how the default legacy template
// renders Git monitoring messages.
func TestDefaultLegacyGitMessages(t *testing.T) {
	t.Parallel()

	got := getTemplatedResult(t, "default-legacy", true, Data{Entries: []*Entry{
		{
			Message: "Found new Git revision",
			Data: map[string]any{
				"revision":     "git.example.com/org/watchtower-git-support-test@v1.2.1",
				"short_commit": "7ab67c773bf4",
			},
		},
		{
			Message: "Built image from Git URL context",
			Data: map[string]any{
				"image":    "wtgit-semver:git-7ab67c773bf4",
				"image_id": "27c196745ab8",
			},
		},
		{
			Message: "Stopping container",
			Data:    map[string]any{"container": "wtgit-semver", "id": "76caeb3ef2ce"},
		},
		{
			Message: "Started new container",
			Data:    map[string]any{"container": "wtgit-semver", "new_id": "0f492bb11f3b"},
		},
	}})
	assert.Equal(t, strings.Join([]string{
		"Found new Git revision: git.example.com/org/watchtower-git-support-test@v1.2.1 (7ab67c773bf4)",
		"Built image: wtgit-semver:git-7ab67c773bf4 (27c196745ab8)",
		"Stopped stale container: wtgit-semver (76caeb3ef2ce)",
		"Started new container: wtgit-semver (0f492bb11f3b)",
	}, "\n"), strings.TrimSpace(got))
}

// TestDefaultLegacyGitSkipAndComposeMessages verifies how the default legacy
// template renders Git skip and Compose messages.
func TestDefaultLegacyGitSkipAndComposeMessages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		message string
		data    map[string]any
		want    string
	}{
		{
			name:    "compose build only",
			message: "Built Compose project",
			data:    map[string]any{"project": "wtgitweb", "service": "api"},
			want:    "Built Compose project: wtgitweb (api)",
		},
		{
			name:    "missing compose dir",
			message: "Compose project directory is not readable. Leaving the running container untouched",
			data: map[string]any{
				"container": "wtgit-missing",
				"error":     "compose project directory is not readable: /srv/missing",
				"image":     "wtgit-app:latest",
			},
			want: "Skipped wtgit-missing: compose directory is not readable",
		},
		{
			name:    "invalid policy",
			message: "Skipped container with an invalid git semver policy",
			data:    map[string]any{"container": "wtgit-app"},
			want:    "Skipped wtgit-app: invalid git semver policy",
		},
		{
			name:    "invalid host",
			message: "Skipped container with an invalid git-host",
			data:    map[string]any{"container": "wtgit-app"},
			want:    "Skipped wtgit-app: invalid git-host",
		},
		{
			name:    "rate limit retries exhausted",
			message: "Registry rate limit retries exhausted. Container failed for this cycle",
			data: map[string]any{
				"container": "sonarr",
				"error":     "failed to pull image: lscr.io/linuxserver/sonarr:latest: registry rate limited: retry-after 347.256µs allowed 44000 per 1m0s",
				"image":     "lscr.io/linuxserver/sonarr:latest",
			},
			want: "Update failed for sonarr (lscr.io/linuxserver/sonarr:latest): failed to pull image: lscr.io/linuxserver/sonarr:latest: registry rate limited: retry-after 347.256µs allowed 44000 per 1m0s",
		},
		{
			name:    "depends-on entry that names no container",
			message: "Ignoring depends-on entry that names no container",
			data:    map[string]any{"container": "web", "depends_on": "missing-db"},
			want:    "Ignored depends-on missing-db for web: no container has that name",
		},
		{
			name:    "self-dependency",
			message: "Skipping container update (self-dependency)",
			data:    map[string]any{"container": "app", "id": "abcdef123456"},
			want:    "Skipped app: depends on itself",
		},
		{
			name:    "circular dependency",
			message: "Skipping container update (circular dependency)",
			data:    map[string]any{"container": "app", "id": "abcdef123456"},
			want:    "Skipped app: circular dependency",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := getTemplatedResult(t, "default-legacy", true, Data{
				Entries: []*Entry{{Message: tc.message, Data: tc.data}},
			})
			assert.Equal(t, tc.want, strings.TrimSpace(got))
			assert.NotContains(t, got, "|")
		})
	}
}

// TestReportCategoryCount verifies that reportCategoryCount adds up the length
// of every category. A container counts once for each list it is in. The count
// is zero for a nil report.
func TestReportCategoryCount(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, reportCategoryCount(nil))

	// One container in each category. Every container but the skipped one is
	// also scanned.
	report := reportfixture.Report(
		state.UpdatedState,
		state.RestartedState,
		state.FailedState,
		state.SkippedState,
		state.StaleState,
		state.FreshState,
	)
	assert.Equal(t, 11, reportCategoryCount(report))
}
