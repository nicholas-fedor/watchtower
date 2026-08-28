// Command e2e is the nested Watchtower end-to-end engine.
//
// This module never imports github.com/nicholas-fedor/watchtower. It drives
// the Watchtower binary or image as a black box inside Testcontainers DinD
// workers, iterates a streamed cartesian product of configuration vectors,
// and writes artifacts under testing/e2e/artifacts/.
//
// Nested layout:
//   - main.go: module entrypoint
//   - cmd: Cobra CLI wiring (flags and output only)
//   - engine: case model, generators, scheduler, checkpoint
//   - docker: DinD workers, inner registry, subjects, probes, envelopes
//   - registry: Hub / GHCR / LSCR persona proxy (no live registry traffic)
//   - watchtower: build and run the binary or container under test
//   - assert: recreate fidelity, porcelain, secrets, HTTP API
//   - report: Markdown, JSON, JUnit, log capture
//   - run: one-case execution and sitting orchestration
package main
