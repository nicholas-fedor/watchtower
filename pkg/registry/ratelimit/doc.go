// Package ratelimit parses registry 429 responses and retries them with backoff.
//
// It honors HTTP Retry-After headers, body quotas, such as "allowed: 44000/minute"
// and Docker pull-stream toomanyrequests messages.
// Advertised quotas are treated as a fill rate.
// After a throttle, each host is limited to one outstanding token so concurrent
// checks cannot dump the advertised budget in a burst.
// In-cycle retries are logged at the debug level and giving up is a warning.
package ratelimit
