package main

import (
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/cmd"
)

// init configures the initial logging level for Watchtower.
//
// It sets logrus to InfoLevel by default, ensuring basic operational logs
// are visible unless overridden by flags like --debug or --log-level in cmd.
func init() {
	logrus.SetLevel(logrus.InfoLevel)
}

// main serves as the entry point for the Watchtower application.
//
// It delegates execution to the cmd package, which handles CLI setup,
// flag parsing, and core logic for container updates and notifications.
func main() {
	cmd.Execute()
}
