package main

import "github.com/nicholas-fedor/watchtower/testing/e2e/cmd"

// main is the nested module entrypoint.
//
// It hands off to cmd.Execute, which runs the Cobra CLI.
func main() {
	cmd.Execute()
}
