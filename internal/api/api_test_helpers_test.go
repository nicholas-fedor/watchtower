package api

import "github.com/rs/zerolog"

// testLogger returns a discarded zerolog logger for API package tests.
func testLogger() *zerolog.Logger {
	l := zerolog.Nop()

	return &l
}
