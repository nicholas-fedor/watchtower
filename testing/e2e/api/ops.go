package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
)

// registerOps mounts health and host operations.
//
// Parameters:
//   - api: Huma API.
//   - svc: Control-plane service.
func registerOps(api huma.API, svc *control.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/v1/health",
		Summary:     "Postgres and Loki readiness",
		Tags:        []string{"ops"},
	}, getHealth(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-host",
		Method:      http.MethodGet,
		Path:        "/v1/host",
		Summary:     "Host CPU/RAM/disk and worker pool",
		Tags:        []string{"ops"},
	}, getHost(svc))
}

// getHealth returns the GET /v1/health handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func getHealth(svc *control.Service) func(context.Context, *struct{}) (*healthOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		out := &healthOutput{}
		out.Status = http.StatusOK
		out.Body.Postgres = "ok"
		out.Body.Loki = "ok"
		out.Body.OK = true

		readyErr := svc.LogsReady(ctx)
		if readyErr != nil {
			out.Body.Loki = readyErr.Error()
			out.Body.OK = false
			out.Status = http.StatusServiceUnavailable
		}

		pingErr := svc.StorePing(ctx)
		if pingErr != nil {
			out.Body.Postgres = pingErr.Error()
			out.Body.OK = false
			out.Status = http.StatusServiceUnavailable
		}

		if loopErr := svc.LoopErr(); loopErr != nil {
			out.Body.Dispatcher = loopErr.Error()
		}

		return out, nil
	}
}

// getHost returns the GET /v1/host handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func getHost(svc *control.Service) func(context.Context, *struct{}) (*hostOutput, error) {
	return func(_ context.Context, _ *struct{}) (*hostOutput, error) {
		snap, err := svc.HostSnapshot("/")
		if err != nil {
			return nil, err
		}

		return &hostOutput{Body: snap}, nil
	}
}
