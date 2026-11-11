package format

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/testutil/golden"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// goldenHost is the hostname used in every golden notification fixture.
const goldenHost = "golden-host"

// goldenEntryTime is the fixed timestamp of every golden log entry.
var goldenEntryTime = time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)

// Errors attached to the failed and skipped containers in the golden report.
var (
	errGoldenFailure = errors.New("pull access denied for org/fail1")
	errGoldenSkipped = errors.New("container is not running")
)

// legacyTemplateMessages lists every log message the default-legacy template
// formats specially, so each of its branches appears in the golden output.
var legacyTemplateMessages = []string{
	logging.FoundNewImage,
	logging.FoundNewGitRevision,
	logging.StoppingContainer,
	logging.StartedNewContainer,
	logging.StoppingLinkedContainer,
	logging.StartedLinkedContainer,
	logging.RemovingImage,
	logging.ImageUsageCheckFailed,
	logging.WithinCooldown,
	logging.CooldownElapsed,
	logging.CooldownUnavailable,
	logging.ExcessInstancesRemoved,
	logging.ImageUsageBudgetEnabled,
	logging.ImageUsageExceedsMaximum,
	logging.ImageUsageExceedsWarning,
	logging.ImageDiskUsageQueryFailed,
	logging.Changelog,
	logging.BuiltGitImage,
	logging.BuiltComposeProject,
	logging.GitBuildFailed,
	logging.ComposeApplyFailed,
	logging.ComposeApplyUnconfirmed,
	logging.ComposeServiceOmitted,
	logging.ComposeReplicaUnidentified,
	logging.ComposeApplySkipped,
	logging.ComposeBatchesCyclic,
	logging.ComposeDirUnreadable,
	logging.ComposeUpdateFailed,
	logging.ComposeBatchSortFailed,
	logging.ComposeBatchOrderFailed,
	logging.InvalidGitHost,
	logging.InvalidGitSemverPolicy,
	logging.SelfUpdateSkippedRunOnce,
	logging.ScopedToContainers,
	logging.StartingAPIServer,
	logging.APIServerEnabled,
	logging.OrphanRecovered,
	logging.OrphanStartFailed,
	logging.RateLimitRetriesExhausted,
	logging.ContainerUpdateFailed,
	logging.UnresolvedDependsOn,
	logging.SelfDependency,
	logging.CircularDependency,
	logging.SelfUnidentified,
	logging.SelfUninspected,
	logging.ScopeLabelUnreadable,
	logging.RestartsKept,
	logging.RunConfigurationFailed,
	logging.RollingRestartValidationFailed,
	logging.APISetupFailed,
	logging.SchedulingFailed,
	logging.InvalidPreUpdateTimeout,
	logging.InvalidPostUpdateTimeout,
	logging.InvalidPreCheckTimeout,
	logging.InvalidPostCheckTimeout,
	logging.UpdatePanicked,
	logging.UpdateFailed,
	logging.UpdateCheckFailed,
	logging.ConfigNotRecreatable,
}

// Disk sizes used by the image-usage budget fixtures, in bytes.
const (
	goldenDiskMax         = int64(10 << 30)
	goldenDiskWarn        = int64(8 << 30)
	goldenDiskReclaimable = int64(2 << 30)
)

// goldenEntryData returns the structured fields attached to a golden log entry
// for message. Each message carries the fields its emitting code attaches, with
// values of the types that code uses, and the disk-usage entries carry usage
// consistent with the threshold they report.
func goldenEntryData(message string) map[string]any {
	diskUsage := func(usage int64) map[string]any {
		return map[string]any{
			"usage":       usage,
			"max":         goldenDiskMax,
			"warn":        goldenDiskWarn,
			"reclaimable": goldenDiskReclaimable,
			"image_count": int64(3),
		}
	}

	switch message {
	case logging.FoundNewImage:
		return map[string]any{"container": "app", "image": "org/app:latest", "new_id": "d0a110000000"}
	case logging.FoundNewGitRevision:
		return map[string]any{
			"container":    "app",
			"revision":     "github.com/org/app@v1.2.0",
			"short_commit": "0123456",
		}
	case logging.StoppingContainer, logging.StoppingLinkedContainer:
		return map[string]any{"container": "app", "id": "c79110000000"}
	case logging.StartedNewContainer, logging.StartedLinkedContainer:
		return map[string]any{"container": "app", "new_id": "d0a110000000"}
	case logging.RemovingImage:
		return map[string]any{
			"container_name": "app",
			"image_name":     "org/app:latest",
			"image_id":       "01d110000000",
		}
	case logging.ImageUsageCheckFailed:
		return map[string]any{
			"image_name": "org/app:latest",
			"image_id":   "01d110000000",
			"error":      "cannot connect to the Docker daemon",
		}
	case logging.WithinCooldown:
		return map[string]any{
			"image":       "org/app:latest",
			"image_age":   "2h",
			"cooldown":    "1d",
			"eligible_in": "22h",
			"eligible_at": "2026-10-08T10:00:00Z",
		}
	case logging.CooldownElapsed:
		return map[string]any{"image": "org/app:latest", "image_age": "2d", "cooldown": "1d"}
	case logging.CooldownUnavailable:
		return map[string]any{
			"image":    "org/app:latest",
			"cooldown": "1d",
			"error":    "manifest unknown",
		}
	case logging.ExcessInstancesRemoved:
		return map[string]any{"removed_instances": 1}
	case logging.ImageUsageBudgetEnabled:
		return map[string]any{"disk_space_max": goldenDiskMax, "disk_space_warn": goldenDiskWarn}
	case logging.ImageUsageExceedsMaximum:
		return diskUsage(int64(11 << 30))
	case logging.ImageUsageExceedsWarning:
		return diskUsage(int64(9 << 30))
	case logging.ImageDiskUsageQueryFailed:
		return map[string]any{"error": "daemon disk usage unavailable"}
	case logging.Changelog:
		return map[string]any{
			"container": "app",
			"changelog": "https://github.com/org/app/releases/tag/v1.2.0",
		}
	case logging.BuiltGitImage:
		return map[string]any{"container": "app", "image": "org/app:latest", "image_id": "b17d10000000"}
	case logging.BuiltComposeProject:
		return map[string]any{"project": "stack", "service": "web"}
	case logging.ComposeApplyUnconfirmed:
		return map[string]any{"project": "stack", "service": "web, worker"}
	case logging.ComposeServiceOmitted,
		logging.ComposeReplicaUnidentified:
		return map[string]any{"container": "stack-web-1", "image": "org/web:latest", "service": "web"}
	case logging.GitBuildFailed,
		logging.ComposeDirUnreadable:
		return map[string]any{"container": "app", "image": "org/app:latest", "error": "example failure"}
	case logging.ComposeApplyFailed,
		logging.ComposeUpdateFailed:
		return map[string]any{
			"container": "stack-web-1",
			"image":     "org/web:latest",
			"dir":       "/srv/stack",
			"error":     "example failure",
		}
	case logging.ComposeApplySkipped,
		logging.InvalidGitHost,
		logging.InvalidGitSemverPolicy,
		logging.SelfUpdateSkippedRunOnce:
		return map[string]any{"container": "app", "image": "org/app:latest"}
	case logging.ComposeBatchSortFailed,
		logging.ComposeBatchOrderFailed:
		return map[string]any{"error": "circular reference detected"}
	case logging.RateLimitRetriesExhausted:
		return map[string]any{
			"container": "app",
			"image":     "org/app:latest",
			"error":     "failed to pull image: org/app:latest: registry rate limited: retry-after 347.256µs allowed 44000 per 1m0s",
		}
	case logging.ContainerUpdateFailed:
		return map[string]any{
			"container": "app",
			"image":     "org/app:latest",
			"error":     "pre-update command failed: exit code 1",
		}
	case logging.UnresolvedDependsOn:
		return map[string]any{"container": "app", "depends_on": "missing-db"}
	case logging.SelfDependency,
		logging.CircularDependency:
		return map[string]any{"container": "app", "id": "abcdef123456"}
	case logging.ScopedToContainers:
		return map[string]any{"scope": "production"}
	case logging.OrphanRecovered:
		return map[string]any{"container": "watchtower", "id": "f00f00f00f00"}
	case logging.OrphanStartFailed:
		return map[string]any{
			"container": "watchtower",
			"id":        "f00f00f00f00",
			"error":     "failed to start container f00f00f00f00: context deadline exceeded",
		}
	case logging.SelfUnidentified:
		return map[string]any{"error": "failed to detect current container ID: no container ID in cgroup file"}
	case logging.SelfUninspected,
		logging.ScopeLabelUnreadable:
		return map[string]any{"error": "failed to inspect container: context deadline exceeded"}
	case logging.RestartsKept:
		return map[string]any{"error": "failed to set restart policy: context deadline exceeded"}
	case logging.RunConfigurationFailed:
		return map[string]any{
			"error": `invalid http-api-host: must be empty or a valid IP address (IPv4 or IPv6): "not-an-ip"`,
		}
	case logging.RollingRestartValidationFailed:
		return map[string]any{"error": "container has dependencies incompatible with rolling restarts"}
	case logging.APISetupFailed:
		return map[string]any{"error": "API token is empty or unset"}
	case logging.SchedulingFailed:
		return map[string]any{"error": `invalid schedule "bad": expected exactly 6 fields, found 1: [bad]`}
	case logging.UpdateFailed:
		return map[string]any{"error": "failed to list containers: context deadline exceeded"}
	case logging.UpdatePanicked:
		return map[string]any{"error": "runtime error: invalid memory address or nil pointer dereference"}
	case logging.ConfigNotRecreatable:
		return map[string]any{
			"container": "app",
			"image":     "org/app:latest",
			"error":     "no image info available",
		}
	case logging.UpdateCheckFailed:
		return map[string]any{
			"container": "app",
			"image":     "org/app:latest",
			"error":     "failed to pull image: org/app:latest: unauthorized: authentication required",
		}
	case logging.InvalidPreUpdateTimeout:
		return map[string]any{
			"container": "app",
			"label":     "com.centurylinklabs.watchtower.lifecycle.pre-update-timeout",
			"value":     "-1",
			"error":     "lifecycle hook timeout must be a whole number of minutes of 0 or more: got -1",
		}
	case logging.InvalidPreCheckTimeout:
		return map[string]any{
			"container": "app",
			"label":     "com.centurylinklabs.watchtower.lifecycle.pre-check-timeout",
			"value":     "1.5",
			"error":     `lifecycle hook timeout must be a whole number of minutes of 0 or more: strconv.Atoi: parsing "1.5": invalid syntax`,
		}
	case logging.InvalidPostCheckTimeout:
		return map[string]any{
			"container": "app",
			"label":     "com.centurylinklabs.watchtower.lifecycle.post-check-timeout",
			"value":     "-5",
			"error":     "lifecycle hook timeout must be a whole number of minutes of 0 or more: got -5",
		}
	case logging.InvalidPostUpdateTimeout:
		return map[string]any{
			"container": "app",
			"label":     "com.centurylinklabs.watchtower.lifecycle.post-update-timeout",
			"value":     "soon",
			"error":     `lifecycle hook timeout must be a whole number of minutes of 0 or more: strconv.Atoi: parsing "soon": invalid syntax`,
		}
	case logging.StartingAPIServer, logging.APIServerEnabled:
		return map[string]any{"host": "0.0.0.0", "port": "8080", "tls": true}
	default:
		return nil
	}
}

// goldenEntries returns one log entry per specially formatted message, plus an
// unrecognized message with and without data, to cover the template fallbacks.
func goldenEntries() []*Entry {
	levels := []string{"info", "warning", "error", "debug"}

	entries := make([]*Entry, 0, len(legacyTemplateMessages)+2)

	for i, message := range legacyTemplateMessages {
		entries = append(entries, &Entry{
			Message: message,
			Data:    goldenEntryData(message),
			Time:    goldenEntryTime.Add(time.Duration(i) * time.Second),
			Level:   levels[i%len(levels)],
		})
	}

	entries = append(entries,
		&Entry{
			Message: "Unrecognized message with fields",
			Data:    map[string]any{"alpha": "one", "beta": 2},
			Time:    goldenEntryTime.Add(time.Hour),
			Level:   "info",
		},
		&Entry{
			Message: "Unrecognized message without fields",
			Time:    goldenEntryTime.Add(2 * time.Hour),
			Level:   "warning",
		},
	)

	return entries
}

// goldenFullReport returns a report with containers in every state. The first
// updated container carries the full set of image and Git metadata templates can show.
func goldenFullReport() *state.Report {
	nop := zerolog.Nop()
	log := &nop
	progress := state.Progress{}
	failures := map[container.ContainerID]error{}

	updated, newImage := reportfixture.Container(0, state.UpdatedState)
	progress.AddScanned(log, updated, newImage, updateParams.Params{})
	progress.MarkForUpdate(log, updated.ID())
	progress.SetReportMeta(log, updated.ID(), provenance.ReportMeta{
		GitRepo:         "https://github.com/org/app.git",
		GitRef:          "v1.2.0",
		Changelog:       "https://github.com/org/app/releases/tag/v1.2.0",
		Source:          "https://github.com/org/app",
		ImageURL:        "https://example.com/org/app",
		Documentation:   "https://example.com/org/app/docs",
		CurrentVersion:  "1.1.0",
		LatestVersion:   "1.2.0",
		CurrentRevision: "1111111111111111111111111111111111111111",
		LatestRevision:  "2222222222222222222222222222222222222222",
	})

	secondUpdated, secondImage := reportfixture.Container(1, state.UpdatedState)
	progress.AddScanned(log, secondUpdated, secondImage, updateParams.Params{})
	progress.MarkForUpdate(log, secondUpdated.ID())

	fresh, _ := reportfixture.Container(0, state.FreshState)
	progress.AddScanned(log, fresh, fresh.ImageID(), updateParams.Params{})

	failed, failedImage := reportfixture.Container(0, state.FailedState)
	progress.AddScanned(log, failed, failedImage, updateParams.Params{})
	failures[failed.ID()] = errGoldenFailure

	skipped, _ := reportfixture.Container(0, state.SkippedState)
	progress.AddSkipped(log, skipped, errGoldenSkipped, updateParams.Params{})

	restarted, _ := reportfixture.Container(0, state.RestartedState)
	progress.AddScanned(log, restarted, restarted.ImageID(), updateParams.Params{})
	progress.MarkRestarted(log, restarted.ID())

	stale, staleImage := reportfixture.Container(0, state.StaleState)
	progress.AddScanned(log, stale, staleImage, updateParams.Params{MonitorOnly: true})

	progress.UpdateFailed(log, failures)

	return progress.Report(log)
}

// goldenFixtures returns the notification data each built-in template is rendered against.
func goldenFixtures() map[string]Data {
	nop := zerolog.Nop()
	staticData := StaticData{
		Title: "Watchtower updates on " + goldenHost,
		Host:  goldenHost,
	}

	return map[string]Data{
		"full": {
			StaticData: staticData,
			Entries:    goldenEntries(),
			Report:     goldenFullReport(),
		},
		"empty": {
			StaticData: staticData,
			Entries:    []*Entry{},
			Report:     state.Progress{}.Report(&nop),
		},
		"nil-report": {
			StaticData: staticData,
			Entries:    goldenEntries(),
			Report:     nil,
		},
	}
}

// TestBuiltInTemplateOutput snapshots the output of every built-in notification
// template for a full report, an empty report, and a missing report.
//
// The legacy template renders the log entries alone, as it does when report
// templates are disabled. Rendering errors are recorded in the golden file so a
// template that starts or stops failing for a fixture is also caught.
func TestBuiltInTemplateOutput(t *testing.T) {
	nop := zerolog.Nop()
	fixtures := goldenFixtures()

	for _, templateName := range slices.Sorted(maps.Keys(commonTemplates)) {
		legacy := templateName == "default-legacy"

		tpl, err := New(&nop, templateName, legacy)
		require.NoError(t, err, "template %q", templateName)

		for _, fixtureName := range slices.Sorted(maps.Keys(fixtures)) {
			t.Run(templateName+"/"+fixtureName, func(t *testing.T) {
				output, err := tpl.Render(&nop, fixtures[fixtureName])
				if err != nil {
					output = "error: " + err.Error()
				}

				if !strings.HasSuffix(output, "\n") {
					output += "\n"
				}

				golden.Assert(t, "template-"+templateName+"-"+fixtureName, []byte(output))
			})
		}
	}
}
