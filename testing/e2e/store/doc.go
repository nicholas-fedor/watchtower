// Package store is the durable record of e2e runs, cases, and events.
//
// Postgres is the system of record. Memory is the unit-test adapter.
// Callers never write checkpoint.json or case directories as truth.
package store
