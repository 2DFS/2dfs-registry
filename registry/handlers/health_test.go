package handlers

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/2DFS/2dfs-registry/v3/configuration"
	"github.com/2DFS/2dfs-registry/v3/health"
	"github.com/2DFS/2dfs-registry/v3/internal/dcontext"
)

func TestFileHealthCheck(t *testing.T) {
	interval := time.Second

	tmpfile, err := os.CreateTemp(os.TempDir(), "healthcheck")
	if err != nil {
		t.Fatalf("could not create temporary file: %v", err)
	}
	defer tmpfile.Close()

	config := &configuration.Configuration{
		Storage: configuration.Storage{
			"inmemory": configuration.Parameters{},
			"maintenance": configuration.Parameters{"uploadpurging": map[interface{}]interface{}{
				"enabled": false,
			}},
		},
		Health: configuration.Health{
			FileCheckers: []configuration.FileChecker{
				{
					Interval: interval,
					File:     tmpfile.Name(),
				},
			},
		},
	}

	ctx := dcontext.Background()

	app := NewApp(ctx, config)
	healthRegistry := health.NewRegistry()
	app.RegisterHealthChecks(healthRegistry)

	// Wait for health check to happen
	<-time.After(2 * interval)

	status := healthRegistry.CheckStatus(ctx)
	if len(status) != 1 {
		t.Fatal("expected 1 item in health check results")
	}
	if status[tmpfile.Name()] != "file exists" {
		t.Fatal(`did not get "file exists" result for health check`)
	}

	os.Remove(tmpfile.Name())

	<-time.After(2 * interval)
	if len(healthRegistry.CheckStatus(ctx)) != 0 {
		t.Fatal("expected 0 items in health check results")
	}
}

func TestTCPHealthCheck(t *testing.T) {
	interval := time.Second

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not create listener: %v", err)
	}
	addrStr := ln.Addr().String()

	// Start accepting
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// listener was closed
				return
			}
			defer conn.Close()
		}
	}()

	config := &configuration.Configuration{
		Storage: configuration.Storage{
			"inmemory": configuration.Parameters{},
			"maintenance": configuration.Parameters{"uploadpurging": map[interface{}]interface{}{
				"enabled": false,
			}},
		},
		Health: configuration.Health{
			TCPCheckers: []configuration.TCPChecker{
				{
					Interval: interval,
					Addr:     addrStr,
					Timeout:  500 * time.Millisecond,
				},
			},
		},
	}

	ctx := dcontext.Background()

	app := NewApp(ctx, config)
	healthRegistry := health.NewRegistry()
	app.RegisterHealthChecks(healthRegistry)

	// Wait for health check to happen
	<-time.After(2 * interval)

	if len(healthRegistry.CheckStatus(ctx)) != 0 {
		t.Fatal("expected 0 items in health check results")
	}

	ln.Close()
	<-time.After(2 * interval)

	// Health check should now fail
	status := healthRegistry.CheckStatus(ctx)
	if len(status) != 1 {
		t.Fatal("expected 1 item in health check results")
	}
	if !strings.Contains(status[addrStr], "connection failed") {
		t.Fatal(`did not get "connection failed" result for health check`)
	}
}

func TestHTTPHealthCheck(t *testing.T) {
	interval := time.Second
	threshold := 3

	stopFailing := make(chan struct{})

	checkedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("expected HEAD request, got %s", r.Method)
		}
		select {
		case <-stopFailing:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	config := &configuration.Configuration{
		Storage: configuration.Storage{
			"inmemory": configuration.Parameters{},
			"maintenance": configuration.Parameters{"uploadpurging": map[interface{}]interface{}{
				"enabled": false,
			}},
		},
		Health: configuration.Health{
			HTTPCheckers: []configuration.HTTPChecker{
				{
					Interval:  interval,
					URI:       checkedServer.URL,
					Threshold: threshold,
				},
			},
		},
	}

	ctx := dcontext.Background()

	app := NewApp(ctx, config)
	healthRegistry := health.NewRegistry()
	app.RegisterHealthChecks(healthRegistry)

	for i := 0; ; i++ {
		<-time.After(interval)

		status := healthRegistry.CheckStatus(ctx)

		if i < threshold-1 {
			// definitely shouldn't have hit the threshold yet
			if len(status) != 0 {
				t.Fatal("expected 1 item in health check results")
			}
			continue
		}
		if i < threshold+1 {
			// right on the threshold - don't expect a failure yet
			continue
		}

		if len(status) != 1 {
			t.Fatal("expected 1 item in health check results")
		}
		if !strings.Contains(status[checkedServer.URL], "downstream service returned unexpected status: 500") {
			t.Fatal("did not get expected result for health check")
		}

		break
	}

	// Signal HTTP handler to start returning 200
	close(stopFailing)

	<-time.After(2 * interval)

	if len(healthRegistry.CheckStatus(ctx)) != 0 {
		t.Fatal("expected 0 items in health check results")
	}
}
