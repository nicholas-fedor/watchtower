package web

import (
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

const (
	// indexRunLimit is sittings shown on the home table.
	indexRunLimit = 50
	// runCaseLimit is cases shown on a sitting page.
	runCaseLimit = 100
)

// Mount registers HTMX dashboard routes on app.
//
// Parameters:
//   - app: Fiber application.
//   - svc: Control plane.
func Mount(app *fiber.App, svc *control.Service) {
	mountStatic(app)
	app.Get("/", handleIndex(svc))
	app.Get("/runs/:id/cases/:cid", handleCase(svc))
	app.Get("/runs/:id", handleRun(svc))
}

// handleIndex serves GET /. Full page unless HX-Request-Type is partial.
//
// Parameters:
//   - svc: Control plane.
//
// Returns:
//   - fiber.Handler: GET /.
func handleIndex(svc *control.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		model := loadIndex(c, svc)
		if htmxPartial(c) {
			return render(c, indexPartial(model))
		}

		return render(c, indexPage(model))
	}
}

// handleRun serves GET /runs/:id. Full page unless HX-Request-Type is partial.
//
// Parameters:
//   - svc: Control plane.
//
// Returns:
//   - fiber.Handler: GET /runs/:id.
func handleRun(svc *control.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		model := loadRun(c, svc)
		if htmxPartial(c) {
			return render(c, runPartial(model))
		}

		return render(c, runPage(model))
	}
}

// htmxPartial reports a targeted HTMX swap (HX-Request-Type: partial).
//
// Boosted links and history restores are "full" and get the complete document.
//
// Parameters:
//   - c: Fiber request context.
//
// Returns:
//   - bool: True when only the fragment should be rendered.
func htmxPartial(c fiber.Ctx) bool {
	return c.Get("HX-Request") == "true" && c.Get("HX-Request-Type") == "partial"
}

// loadIndex reads the home-page model.
//
// Parameters:
//   - c: Fiber request context.
//   - svc: Control plane.
//
// Returns:
//   - indexModel: Runs list and host snapshot.
func loadIndex(c fiber.Ctx, svc *control.Service) indexModel {
	model := indexModel{}

	snap, hostErr := svc.HostSnapshot("/")
	if hostErr == nil {
		model.Host = snap
		model.HostOK = true
	}

	runs, err := svc.ListRuns(c.Context(), store.RunListFilter{Limit: indexRunLimit})
	if err != nil {
		model.Err = err.Error()

		return model
	}

	model.Runs = runs

	return model
}

// loadRun reads one sitting page model.
//
// Parameters:
//   - c: Fiber request context. Path id and query status are read.
//   - svc: Control plane.
//
// Returns:
//   - runModel: Sitting snapshot and cases.
func loadRun(c fiber.Ctx, svc *control.Service) runModel {
	id := c.Params("id")
	model := runModel{}

	run, err := svc.GetRun(c.Context(), id)
	if err != nil {
		model.Err = err.Error()

		return model
	}

	model.Run = run
	model.Status = c.Query("status")

	filter := store.CaseListFilter{Limit: runCaseLimit}
	if model.Status != "" {
		filter.Status = store.CaseStatus(model.Status)
	}

	cases, total, listErr := svc.ListCases(c.Context(), id, filter)
	if listErr != nil {
		model.Err = listErr.Error()

		return model
	}

	model.Cases = cases
	model.Total = total

	return model
}
