// Package framework provides Git testing utilities for Watchtower e2e tests.
package framework

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// MockGitServer manages a mock Git server for testing Git monitoring functionality.
type MockGitServer struct {
	container testcontainers.Container
	url       string
	repoPath  string
}

// NewMockGitServer creates and starts a mock Git server container.
func NewMockGitServer(ctx context.Context, repoName string) (*MockGitServer, error) {
	req := testcontainers.ContainerRequest{
		Image: "alpine/git:latest",
		Cmd: []string{
			"sh",
			"-c",
			"git daemon --reuseaddr --base-path=/git --export-all --enable=receive-pack --listen=0.0.0.0 --port=9418",
		},
		ExposedPorts: []string{"9418/tcp"},
		WaitingFor:   wait.ForListeningPort("9418/tcp").WithStartupTimeout(60 * time.Second),
		AutoRemove:   true,
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      "/tmp/mock-repo.tar.gz", // We'll create this
				ContainerFilePath: "/git/mock-repo.tar.gz",
			},
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start Git server container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)

		return nil, fmt.Errorf("failed to get Git server host: %w", err)
	}

	port, err := container.MappedPort(ctx, "9418")
	if err != nil {
		container.Terminate(ctx)

		return nil, fmt.Errorf("failed to get Git server port: %w", err)
	}

	url := fmt.Sprintf("git://%s:%s/%s.git", host, port.Port(), repoName)
	repoPath := fmt.Sprintf("/git/%s.git", repoName)

	server := &MockGitServer{
		container: container,
		url:       url,
		repoPath:  repoPath,
	}

	log.Printf("Mock Git server started at: %s", url)

	return server, nil
}

// URL returns the Git repository URL for cloning.
func (s *MockGitServer) URL() string {
	return s.url
}

// SetupMockRepo initializes a mock Git repository with the specified branch and commit.
func (s *MockGitServer) SetupMockRepo(ctx context.Context, branch, initialCommit string) error {
	// Create initial repository structure
	setupCmd := exec.Command("docker", "exec", s.container.GetContainerID(),
		"sh", "-c", fmt.Sprintf(`
			mkdir -p %s &&
			cd %s &&
			git init --bare &&
			git config --global user.email "test@example.com" &&
			git config --global user.name "Test User"
		`, s.repoPath, s.repoPath))

	if output, err := setupCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to setup mock repo: %w, output: %s", err, string(output))
	}

	// Create a working directory and push initial commit
	workCmd := exec.Command("docker", "exec", s.container.GetContainerID(),
		"sh", "-c", fmt.Sprintf(`
			mkdir -p /tmp/work && cd /tmp/work &&
			git clone %s . &&
			echo "Initial commit" > README.md &&
			git add README.md &&
			git commit -m "%s" &&
			git push origin %s
		`, s.url, initialCommit, branch))

	if output, err := workCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create initial commit: %w, output: %s", err, string(output))
	}

	return nil
}

// SimulateGitCommit creates a new commit in the mock repository.
func (s *MockGitServer) SimulateGitCommit(ctx context.Context, commitMessage string) error {
	// Create a new commit by modifying a file and pushing
	commitCmd := exec.Command("docker", "exec", s.container.GetContainerID(),
		"sh", "-c", fmt.Sprintf(`
			cd /tmp/work &&
			echo "%s - $(date)" >> changes.txt &&
			git add changes.txt &&
			git commit -m "%s" &&
			git push origin main
		`, commitMessage, commitMessage))

	if output, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to simulate Git commit: %w, output: %s", err, string(output))
	}

	return nil
}

// Cleanup stops and removes the Git server container.
func (s *MockGitServer) Cleanup(ctx context.Context) error {
	timeout := 30 * time.Second

	return s.container.Stop(ctx, &timeout)
}

// SetupMockGitRepo creates a mock Git repository for testing.
// This is a simplified version that creates a local Git repository.
func (f *E2EFramework) SetupMockGitRepo(name, branch, initialCommit string) (string, func() error) {
	// Create a temporary directory for the mock repo
	tempDir := fmt.Sprintf("/tmp/watchtower-git-test-%d", time.Now().Unix())
	repoPath := filepath.Join(tempDir, name+".git")

	// Initialize bare repository
	initCmd := exec.Command("git", "init", "--bare", repoPath)
	if err := initCmd.Run(); err != nil {
		log.Printf("Failed to init mock repo: %v", err)

		return "", func() error { return nil }
	}

	// Create a working copy and make initial commit
	workDir := strings.TrimSuffix(repoPath, ".git")
	cloneCmd := exec.Command("git", "clone", repoPath, workDir)
	if err := cloneCmd.Run(); err != nil {
		log.Printf("Failed to clone mock repo: %v", err)

		return "", func() error { return nil }
	}

	// Configure git user
	configCmd1 := exec.Command("git", "-C", workDir, "config", "user.email", "test@example.com")
	configCmd2 := exec.Command("git", "-C", workDir, "config", "user.name", "Test User")
	configCmd1.Run()
	configCmd2.Run()

	// Create initial file and commit
	fileCmd := exec.Command(
		"sh",
		"-c",
		fmt.Sprintf(`echo "%s" > %s/README.md`, initialCommit, workDir),
	)
	fileCmd.Run()

	addCmd := exec.Command("git", "-C", workDir, "add", "README.md")
	addCmd.Run()

	commitCmd := exec.Command("git", "-C", workDir, "commit", "-m", initialCommit)
	commitCmd.Run()

	pushCmd := exec.Command("git", "-C", workDir, "push", "origin", branch)
	pushCmd.Run()

	// Return file:// URL for local access
	repoURL := fmt.Sprintf("file://%s", repoPath)

	cleanup := func() error {
		return exec.Command("rm", "-rf", tempDir).Run()
	}

	f.addCleanupFunc(cleanup)

	return repoURL, cleanup
}

// SimulateGitCommit creates a new commit in the test Git repository.
func (f *E2EFramework) SimulateGitCommit(repoURL, commitMessage string) error {
	// Extract repo path from file:// URL
	if !strings.HasPrefix(repoURL, "file://") {
		return fmt.Errorf("only file:// URLs supported for mock commits")
	}

	repoPath := strings.TrimPrefix(repoURL, "file://")
	workDir := strings.TrimSuffix(repoPath, ".git")

	// Create a new file/modify existing one
	fileCmd := exec.Command(
		"sh",
		"-c",
		fmt.Sprintf(`echo "%s - $(date)" >> %s/changes.txt`, commitMessage, workDir),
	)
	if output, err := fileCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create test file: %w, output: %s", err, string(output))
	}

	// Add, commit, and push
	addCmd := exec.Command("git", "-C", workDir, "add", "changes.txt")
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add file: %w, output: %s", err, string(output))
	}

	commitCmd := exec.Command("git", "-C", workDir, "commit", "-m", commitMessage)
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to commit: %w, output: %s", err, string(output))
	}

	pushCmd := exec.Command("git", "-C", workDir, "push", "origin", "main")
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push: %w, output: %s", err, string(output))
	}

	return nil
}
