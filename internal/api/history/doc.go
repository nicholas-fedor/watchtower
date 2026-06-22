// Package history provides the /v1/history HTTP API endpoint, exposing
// historical scan results from the in-memory ring buffer (up to 500 entries).
// Results can be filtered by time range (?since=, ?until=) and limited by count (?limit=).
package history
