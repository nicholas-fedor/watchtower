// Package ratelimit parses registry 429 responses and retries them with backoff.
//
// It honors HTTP Retry-After headers, body quotas, such as "allowed: 44000/minute"
// and Docker pull-stream toomanyrequests messages.
// Advertised quotas are treated as a fill rate.
// After a throttle, each host is limited to one outstanding token so concurrent
// checks cannot dump the advertised budget in a burst.
// Unauthenticated GHCR traffic is serialized from the first request onto a
// separate limiter key from [Scope] so anonymous 429s cannot pace authenticated
// checks. Token-bucket waits retry until the honor window. In-cycle retries are
// debug. Giving up is a warning.
package ratelimit
