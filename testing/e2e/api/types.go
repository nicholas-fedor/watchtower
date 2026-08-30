package api

import (
	"github.com/nicholas-fedor/watchtower/testing/e2e/host"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

// idInput is the path id for sitting operations.
type idInput struct {
	ID string `doc:"Run UUID" path:"id"`
}

// caseInput is the path for one case.
type caseInput struct {
	ID     string `doc:"Run UUID"        path:"id"`
	CaseID string `doc:"Case identifier" path:"cid"`
}

// createRunInput is POST /v1/runs.
type createRunInput struct {
	Body store.Spec
}

// runOutput is one sitting JSON body.
type runOutput struct {
	Body store.Run
}

// runsOutput is GET /v1/runs.
type runsOutput struct {
	Body struct {
		Runs []store.Run `json:"runs"`
	}
}

// listRunsInput is GET /v1/runs query.
type listRunsInput struct {
	Status string `doc:"Filter by run status" enum:"queued,running,interrupted,completed,failed,canceled" query:"status"`
	Limit  int    `doc:"Page size"                                                                        query:"limit"  maximum:"200" minimum:"1"`
	Offset int    `doc:"Offset"                                                                           query:"offset"               minimum:"0"`
}

// listCasesInput is GET /v1/runs/{id}/cases query.
type listCasesInput struct {
	ID     string `path:"id"`
	Status string `          doc:"Case status"                           enum:"pending,running,pass,fail,skip,interrupted" query:"status"`
	Q      string `          doc:"Substring match on case id or factors"                                                   query:"q"`
	Limit  int    `          doc:"Page size"                                                                               query:"limit"  maximum:"200" minimum:"1"`
	Offset int    `          doc:"Offset"                                                                                  query:"offset"               minimum:"0"`
}

// casesOutput is GET /v1/runs/{id}/cases.
type casesOutput struct {
	Body struct {
		Cases []store.Case `json:"cases"`
		Total int          `json:"total"`
	}
}

// caseOutput is GET /v1/runs/{id}/cases/{cid}.
type caseOutput struct {
	Body store.Case
}

// logsInput is GET /v1/runs/{id}/cases/{cid}/logs query.
type logsInput struct {
	ID     string `path:"id"`
	CaseID string `path:"cid"`
	Stream string `           doc:"Restrict to one stream" enum:"stdout,stderr" query:"stream"`
}

// logsOutput is GET /v1/runs/{id}/cases/{cid}/logs.
type logsOutput struct {
	Body struct {
		Lines []stream.Line `json:"lines"`
	}
}

// healthOutput is GET /v1/health.
type healthOutput struct {
	Status int `status:"true"`
	Body   struct {
		OK         bool   `json:"ok"`
		Postgres   string `json:"postgres"`
		Loki       string `json:"loki"`
		Dispatcher string `json:"dispatcher,omitempty"`
	}
}

// hostOutput is GET /v1/host.
type hostOutput struct {
	Body host.Snapshot
}

// eventsInput is GET /v1/runs/{id}/events.
type eventsInput struct {
	ID          string `path:"id"`
	LastEventID string `          header:"Last-Event-ID"`
}

// junitOutput is GET /v1/runs/{id}/junit.
type junitOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}
