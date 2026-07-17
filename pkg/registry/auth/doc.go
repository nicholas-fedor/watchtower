// Package auth implements the Docker Registry HTTP API V2 authentication protocol.
//
// Flow:
//  1. GET /v2/ → 401 + WWW-Authenticate header
//  2. Parse challenge (realm, service, scope)
//  3. Fetch bearer token from realm endpoint (with caching)
//  4. Return token for use in subsequent requests
//
// Special cases:
//   - lscr.io images are hosted on ghcr.io, so auth challenges are remapped
//   - Docker Hub's docker.io maps to index.docker.io
//   - Basic auth is supported when credentials are provided
package auth
