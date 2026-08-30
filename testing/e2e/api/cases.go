package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// registerCases mounts case list, get, and logs operations.
//
// Parameters:
//   - api: Huma API.
//   - svc: Control-plane service.
func registerCases(api huma.API, svc *control.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "list-cases",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}/cases",
		Summary:     "List cases in a sitting",
		Tags:        []string{"cases"},
	}, listCases(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-case",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}/cases/{cid}",
		Summary:     "Get one case including documents",
		Tags:        []string{"cases"},
	}, getCase(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-case-logs",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}/cases/{cid}/logs",
		Summary:     "Query Loki (or memory) logs for a case",
		Tags:        []string{"cases"},
	}, getCaseLogs(svc))
}

// listCases returns the GET /v1/runs/{id}/cases handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func listCases(svc *control.Service) func(context.Context, *listCasesInput) (*casesOutput, error) {
	return func(ctx context.Context, input *listCasesInput) (*casesOutput, error) {
		items, total, err := svc.ListCases(ctx, input.ID, store.CaseListFilter{
			Status: store.CaseStatus(input.Status),
			Query:  input.Q,
			Limit:  input.Limit,
			Offset: input.Offset,
		})
		if err != nil {
			return nil, mapStoreErr(err)
		}

		out := &casesOutput{}
		out.Body.Cases = items
		out.Body.Total = total

		return out, nil
	}
}

// getCase returns the GET /v1/runs/{id}/cases/{cid} handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func getCase(svc *control.Service) func(context.Context, *caseInput) (*caseOutput, error) {
	return func(ctx context.Context, input *caseInput) (*caseOutput, error) {
		item, err := svc.GetCase(ctx, input.ID, input.CaseID)
		if err != nil {
			return nil, mapStoreErr(err)
		}

		return &caseOutput{Body: item}, nil
	}
}

// getCaseLogs returns the GET /v1/runs/{id}/cases/{cid}/logs handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma handler.
func getCaseLogs(svc *control.Service) func(context.Context, *logsInput) (*logsOutput, error) {
	return func(ctx context.Context, input *logsInput) (*logsOutput, error) {
		lines, err := svc.QueryLogs(ctx, input.ID, input.CaseID, input.Stream)
		if err != nil {
			return nil, err
		}

		out := &logsOutput{}
		out.Body.Lines = lines

		return out, nil
	}
}
