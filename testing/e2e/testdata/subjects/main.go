// Subject is a tiny HTTP process used as a Watchtower update target.
//
// Build with CGO_ENABLED=0 GOOS=linux and copy into a scratch image. Behavior
// is selected by SUBJECT_KIND: echo, slow-term, deaf-term, custom-signal,
// healthcheck, nonroot, volume-writer.
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// listenAddr is the dummy HTTP bind address.
const listenAddr = ":8080"

// main starts the HTTP server and waits for the signal appropriate to SUBJECT_KIND.
func main() {
	kind := os.Getenv("SUBJECT_KIND")
	if kind == "" {
		kind = "echo"
	}

	tag := os.Getenv("TAG")
	if tag == "" {
		tag = "r1"
	}

	rev := os.Getenv("REV")
	if rev == "" {
		rev = "1"
	}

	if os.Getenv("VOLUME_PATH") != "" {
		path := os.Getenv("VOLUME_PATH") + "/written"
		_ = os.WriteFile(path, []byte(tag+" "+rev), 0o644)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(writer, "%s %s", tag, rev)
	})
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})

	go func() {
		_ = http.ListenAndServe(listenAddr, mux)
	}()

	handleSignals(kind)
}

// handleSignals blocks until the process should exit for the given subject kind.
//
// Parameters:
//   - kind: SUBJECT_KIND value.
func handleSignals(kind string) {
	switch kind {
	case "deaf-term":
		signal.Ignore(syscall.SIGTERM)
		waitFor(syscall.SIGKILL)
	case "slow-term":
		waitTermThenSleep(15 * time.Second)
	case "custom-signal":
		signal.Ignore(syscall.SIGTERM)
		waitFor(syscall.SIGHUP)
	default:
		waitFor(syscall.SIGTERM, syscall.SIGINT)
	}
}

// waitFor blocks until one of the given signals is received.
//
// Parameters:
//   - signals: OS signals to wait on.
func waitFor(signals ...os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	<-ch
}

// waitTermThenSleep waits for SIGTERM, then sleeps before exiting.
//
// Parameters:
//   - delay: How long to linger after SIGTERM.
func waitTermThenSleep(delay time.Duration) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	<-ch
	time.Sleep(delay)
}
