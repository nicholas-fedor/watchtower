// Package ui provides the HTTP handlers for Watchtower's built-in web UI.
//
// The UI is a static single-page application embedded in the binary and
// served under /ui/. It has no data endpoints of its own: once signed in, the
// page calls the regular /v1/* endpoints from the same origin and shows only
// the features whose endpoints are enabled.
//
// Sign-in exchanges the HTTP API token for an HttpOnly, SameSite=Strict
// access_token cookie, which the API auth middleware already accepts. The
// token therefore never has to be stored where page scripts can read it.
//
// Routes:
//
//	GET    /ui/           Application shell
//	GET    /ui/assets/*   Embedded scripts, styles, and fonts
//	GET    /ui/session    Sign-in state and enabled features
//	POST   /ui/session    Sign in with the HTTP API token
//	DELETE /ui/session    Sign out
package ui
