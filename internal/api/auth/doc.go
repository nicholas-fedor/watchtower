// Package auth authenticates HTTP API requests by the token they carry.
//
// The API and events tokens are checked the same way. They differ only in the
// fallback source read after the Authorization header, because browsers cannot
// set headers on an EventSource connection.
package auth
