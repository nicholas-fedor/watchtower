// Package schedule runs container updates on a schedule.
//
// It serializes scheduled updates with updates triggered through the HTTP API,
// and lets a running update finish when the process shuts down.
package schedule
