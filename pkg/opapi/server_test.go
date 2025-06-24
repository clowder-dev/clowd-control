package opapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLogger creates a logger instance for tests, discarding output.
func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func TestNewServer(t *testing.T) {
	cfg := Config{
		ListenAddress: ":0", // Use :0 for random available port in tests
		Logger:        newTestLogger(),
	}
	s, err := NewServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.NotNil(t, s.httpServer)
	assert.NotNil(t, s.logger)
	assert.Equal(t, cfg.ListenAddress, s.httpServer.Addr)
}

func TestNewServer_DefaultLogger(t *testing.T) {
	cfg := Config{
		ListenAddress: ":0",
		Logger:        nil, // Test default logger creation
	}
	s, err := NewServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.NotNil(t, s.logger, "Server should have a default logger if none provided")
}

func TestServer_HealthCheckHandler(t *testing.T) {
	cfg := Config{
		ListenAddress: ":0",
		Logger:        newTestLogger(),
	}
	s, err := NewServer(cfg)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/health", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	// Serve the request using the server's main router
	s.httpServer.Handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "Health check should return status OK")
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status": "ok"}`, strings.TrimSpace(rr.Body.String()), "Health check response body mismatch")
}

func TestServer_StartAndStop(t *testing.T) {
	// Create a listener on a random port to get a free address
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Failed to create listener")
	listenAddr := listener.Addr().String()
	// Close the initial listener as the server's ListenAndServe will bind to this address.
	require.NoError(t, listener.Close(), "Failed to close temporary listener")

	cfg := Config{
		ListenAddress: listenAddr,
		Logger:        newTestLogger(),
	}
	s, err := NewServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, s)

	var wg sync.WaitGroup
	wg.Add(1) // For the server goroutine

	go func() {
		defer wg.Done()
		serverErr := s.Start()
		// We expect ErrServerClosed on graceful shutdown. Any other error is a problem.
		if serverErr != nil && serverErr != http.ErrServerClosed {
			t.Errorf("s.Start() returned an unexpected error: %v", serverErr)
		}
	}()

	// Wait for the server to be available by polling the health endpoint
	var resp *http.Response
	var healthErr error
	client := http.Client{Timeout: 200 * time.Millisecond}
	for i := 0; i < 25; i++ { // Poll for up to 2.5 seconds
		resp, healthErr = client.Get("http://" + listenAddr + "/api/v1/health")
		if healthErr == nil && resp != nil && resp.StatusCode == http.StatusOK {
			break // Server is up
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close() // Important to close body even on failed attempts
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.NoError(t, healthErr, "Health check request failed after retries")
	require.NotNil(t, resp, "Health check response was nil")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return OK after server starts")
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopErr := s.Stop(ctx)
	assert.NoError(t, stopErr, "s.Stop() should not return an error")

	wg.Wait() // Wait for the Start goroutine to finish (ListenAndServe to return ErrServerClosed)
}
