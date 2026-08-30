// Package cmd is the Cobra CLI for the Watchtower e2e engine.
//
// Command files parse flags and print results. The control-plane process lives
// in server. JSON HTTP lives in api. Sitting execution, generators, DinD
// probes, and persona serving live in run, engine, docker, and registry.
package cmd
