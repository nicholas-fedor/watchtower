// Package events provides the /v1/events HTTP API endpoint for real-time
// Server-Sent Events (SSE). It exposes a Broadcaster that manages subscriber
// registration and event distribution, and a Handler that streams Watchtower
// operational events (scan started/completed, update started/completed/failed)
// to connected clients.
package events
