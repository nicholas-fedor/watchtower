// Package web is the HTMX dashboard served from the same Fiber app as /v1.
//
// Markup lives in templ files.
// Routes load models and render.
// Styles are Tailwind v4 (static/input.css → static/app.css via `make css`).
// htmx 4.0.0 is vendored under static/.
// Targeted requests (HX-Request-Type: partial) get a fragment.
// Boosted links and browsers get the full page.
//
//nolint:godoclint // templ-generated files put comments above package.
package web
