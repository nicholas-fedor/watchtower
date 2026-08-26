// Package ratelimit parses registry 429 responses and retries them with backoff.
//
// It honors HTTP Retry-After headers, GHCR-style body quotas such as
// "allowed: 44000/minute", and Docker pull-stream toomanyrequests messages.
package ratelimit
