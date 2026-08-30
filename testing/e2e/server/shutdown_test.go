package server

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestShutdownAppUnblocksListener(t *testing.T) {
	app := fiber.New()
	started := make(chan struct{})
	app.Get("/hang", func(c fiber.Ctx) error {
		close(started)
		<-c.Context().Done()

		return nil
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listener(ln)
	}()

	go func() {
		cli := &http.Client{Timeout: time.Second}
		_, _ = cli.Get("http://" + ln.Addr().String() + "/hang")
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	require.NoError(t, shutdownApp(app, 200*time.Millisecond))

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Listener did not return after shutdown")
	}
}
