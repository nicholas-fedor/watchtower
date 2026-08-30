// Package infra starts durable Postgres and Loki via Docker Compose.
//
// Containers are named and volume-backed. They are not Testcontainers
// resources. Ryuk must not own them. Host dockerd configuration is not
// modified.
package infra
