// Package main provides the entry point for the e2e test suite.
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

const minArgs = 2

// main runs the e2e test suite based on command line arguments.

func main() {
	if len(os.Args) < minArgs {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "build":
		buildImage()
	case "test":
		runTests()
	case "run":
		buildImage()
		runTests()
	case "cleanup":
		cleanup()
	default:
		log.Printf("Unknown command: %s", command)
		printUsage()
		os.Exit(1)
		// printUsage displays the help message for the e2e test suite.
	}
}

func printUsage() {
	log.Println("Usage: go run main.go <command>")
	log.Println("Commands:")
	log.Println("  build   - Build the local Watchtower Docker image")
	log.Println("  test    - Run the e2e test suite")
	// buildImage builds the local Watchtower Docker image.
	log.Println("  run     - Build image and run tests")
	log.Println("  cleanup - Clean up local environment")
}

func buildImage() {
	log.Println("🔨 Building Watchtower Docker image...")

	frameworkInstance, err := framework.NewE2EFramework("dummy")
	if err != nil {
		log.Fatalf("Failed to create framework: %v", err)
	}

	log.Println("📦 Building image watchtower:test")

	err = frameworkInstance.BuildWatchtowerImage("watchtower", "test")
	if err != nil {
		log.Fatalf("Failed to build image: %v", err)
	}

	log.Println("✅ Image built successfully: watchtower:test")

	// Verify the image
	log.Println("🔍 Verifying image...")

	cmd := exec.CommandContext(
		context.Background(),
		"docker",
		"run",
		"--rm",
		"watchtower:test",
		"--help",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Image verification failed: %v\nOutput: %s", err, string(output))
		// runTests executes the e2e test suite using go test.
	}

	log.Println("✅ Image verification passed")
}

func runTests() {
	log.Println("🧪 Running e2e test suite...")

	cmd := exec.CommandContext(context.Background(), "go", "test", "./test/e2e/...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		// cleanup performs cleanup of local Docker environment.
		log.Fatalf("Tests failed: %v", err)
	}

	log.Println("✅ Tests completed")
}

func cleanup() {
	log.Println("🧹 Cleaning up local environment...")

	// Remove the test image
	log.Println("🗑️  Removing test image watchtower:test")

	cmd := exec.CommandContext(context.Background(), "docker", "rmi", "watchtower:test")

	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such image") {
			log.Printf("Warning: Failed to remove image: %v\nOutput: %s", err, string(output))
		}
	} else {
		log.Println("✅ Image removed")
	}

	// Remove unused images
	log.Println("🗑️  Removing unused images")

	cmd = exec.CommandContext(context.Background(), "docker", "image", "prune", "-a", "-f")

	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: Failed to prune images: %v\nOutput: %s", err, string(output))
	} else {
		log.Println("✅ Unused images removed")
	}

	// Remove stopped containers
	log.Println("🗑️  Removing stopped containers")

	cmd = exec.CommandContext(context.Background(), "docker", "container", "prune", "-f")

	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: Failed to prune containers: %v\nOutput: %s", err, string(output))
	} else {
		log.Println("✅ Stopped containers removed")
	}

	// Remove unused volumes
	log.Println("🗑️  Removing unused volumes")

	cmd = exec.CommandContext(context.Background(), "docker", "volume", "prune", "-f")

	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: Failed to prune volumes: %v\nOutput: %s", err, string(output))
	} else {
		log.Println("✅ Unused volumes removed")
	}

	log.Println("✅ Cleanup completed")
}
