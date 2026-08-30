// Package api is the Huma/Fiber JSON control plane at /v1.
//
// The CLI and the HTMX dashboard are clients of this API. OpenAPI is served
// at /v1/openapi.json. Handlers stay in per-resource files so this package
// does not grow a single registration dump.
package api
