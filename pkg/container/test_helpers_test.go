package container

import (
	"fmt"
	"net/http"
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

// testLog returns a discarded zerolog logger for tests that do not assert on output.
func testLog() *zerolog.Logger {
	return logging.NopLogger()
}

// captureLog returns a logfmt *zerolog.Logger writing to a gbytes.Buffer for gomega.Say assertions.
func captureLog(level zerolog.Level) (*zerolog.Logger, *gbytes.Buffer) {
	buf := gbytes.NewBuffer()
	w := logging.LogfmtWriter(buf)
	// Preserve timestamp style used by production logfmt.
	w.TimeFormat = time.RFC3339
	l := zerolog.New(w).Level(level).With().Timestamp().Logger()

	return &l, buf
}

// missingImageHandlers mocks a running container whose image can no longer be
// inspected, which happens when the image is removed out from under it.
//
// The container inspect succeeds and the image inspect returns 404, so callers
// end up with a container whose HasImageInfo reports false.
func missingImageHandlers(containerID string) []http.HandlerFunc {
	return []http.HandlerFunc{
		ghttp.CombineHandlers(
			ghttp.VerifyRequest(
				"GET",
				gomega.MatchRegexp(
					fmt.Sprintf("^/v[0-9.]+/containers/%s/json$", containerID),
				),
			),
			ghttp.RespondWithJSONEncoded(http.StatusOK, dockerContainer.InspectResponse{
				ID:    containerID,
				Name:  "/test-container",
				Image: "test-image:latest",
				State: &dockerContainer.State{
					Status:  "running",
					Running: true,
				},
				HostConfig: &dockerContainer.HostConfig{},
				Config: &dockerContainer.Config{
					Image: "test-image:latest",
				},
			}),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest(
				"GET",
				gomega.MatchRegexp("^/v[0-9.]+/images/test-image:latest/json$"),
			),
			ghttp.RespondWith(http.StatusNotFound, `{"message":"No such image"}`),
		),
	}
}
