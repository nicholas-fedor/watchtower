package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// registerRuns mounts sitting CRUD, cancel, resume, and JUnit export.
//
// Parameters:
//   - api: Huma API.
//   - svc: Control-plane service.
func registerRuns(api huma.API, svc *control.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "create-run",
		Method:      http.MethodPost,
		Path:        "/v1/runs",
		Summary:     "Queue a sitting",
		Tags:        []string{"runs"},
	}, createRun(svc))

	huma.Register(api, huma.Operation{
		OperationID: "list-runs",
		Method:      http.MethodGet,
		Path:        "/v1/runs",
		Summary:     "List sittings, newest first",
		Tags:        []string{"runs"},
	}, listRuns(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-run",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}",
		Summary:     "Get one sitting",
		Tags:        []string{"runs"},
	}, getRun(svc))

	huma.Register(api, huma.Operation{
		OperationID: "cancel-run",
		Method:      http.MethodPost,
		Path:        "/v1/runs/{id}/cancel",
		Summary:     "Cancel a queued or running sitting",
		Tags:        []string{"runs"},
	}, cancelRun(svc))

	huma.Register(api, huma.Operation{
		OperationID: "resume-run",
		Method:      http.MethodPost,
		Path:        "/v1/runs/{id}/resume",
		Summary:     "Re-queue an interrupted sitting",
		Tags:        []string{"runs"},
	}, resumeRun(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-junit",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}/junit",
		Summary:     "Derived JUnit XML for a sitting",
		Tags:        []string{"runs"},
	}, getJUnit(svc))
}

// createRun returns the POST /v1/runs handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func createRun(svc *control.Service) func(context.Context, *createRunInput) (*runOutput, error) {
	return func(ctx context.Context, input *createRunInput) (*runOutput, error) {
		label := time.Now().UTC().Format("20060102T150405Z")

		run, err := svc.CreateRun(ctx, input.Body, label)
		if err != nil {
			return nil, mapStoreErr(err)
		}

		return &runOutput{Body: run}, nil
	}
}

// listRuns returns the GET /v1/runs handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func listRuns(svc *control.Service) func(context.Context, *listRunsInput) (*runsOutput, error) {
	return func(ctx context.Context, input *listRunsInput) (*runsOutput, error) {
		runs, err := svc.ListRuns(ctx, store.RunListFilter{
			Status: store.RunStatus(input.Status),
			Limit:  input.Limit,
			Offset: input.Offset,
		})
		if err != nil {
			return nil, err
		}

		out := &runsOutput{}
		out.Body.Runs = runs

		return out, nil
	}
}

// getRun returns the GET /v1/runs/{id} handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func getRun(svc *control.Service) func(context.Context, *idInput) (*runOutput, error) {
	return func(ctx context.Context, input *idInput) (*runOutput, error) {
		run, err := svc.GetRun(ctx, input.ID)
		if err != nil {
			return nil, mapStoreErr(err)
		}

		return &runOutput{Body: run}, nil
	}
}

// cancelRun returns the POST /v1/runs/{id}/cancel handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func cancelRun(svc *control.Service) func(context.Context, *idInput) (*runOutput, error) {
	return func(ctx context.Context, input *idInput) (*runOutput, error) {
		run, err := svc.Cancel(ctx, input.ID)
		if err != nil {
			return nil, mapStoreErr(err)
		}

		return &runOutput{Body: run}, nil
	}
}

// resumeRun returns the POST /v1/runs/{id}/resume handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func resumeRun(svc *control.Service) func(context.Context, *idInput) (*runOutput, error) {
	return func(ctx context.Context, input *idInput) (*runOutput, error) {
		run, err := svc.Resume(ctx, input.ID)
		if err != nil {
			return nil, mapStoreErr(err)
		}

		return &runOutput{Body: run}, nil
	}
}

// getJUnit returns the GET /v1/runs/{id}/junit handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func getJUnit(svc *control.Service) func(context.Context, *idInput) (*junitOutput, error) {
	return func(ctx context.Context, input *idInput) (*junitOutput, error) {
		run, err := svc.GetRun(ctx, input.ID)
		if err != nil {
			return nil, mapStoreErr(err)
		}

		items, _, listErr := svc.ListCases(ctx, input.ID, store.CaseListFilter{Limit: store.MaxPageSize})
		if listErr != nil {
			return nil, listErr
		}

		return &junitOutput{
			ContentType: "application/xml",
			Body:        []byte(junitXML(run, items)),
		}, nil
	}
}
