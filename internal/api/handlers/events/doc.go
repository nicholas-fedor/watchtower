// Package events provides the /v1/events HTTP API endpoint for real-time
// Server-Sent Events (SSE). It exposes a Broadcaster that manages subscriber
// registration and event distribution, and a Handler that streams Watchtower
// operational events (scan_started, scan_failed, image_cleanup, scan_completed)
// to connected clients.
package events
