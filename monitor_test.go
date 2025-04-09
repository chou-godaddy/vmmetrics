package metrics_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/VictoriaMetrics/metrics"
    "github.com/stretchr/testify/assert"
)

func TestNewVMMetricsMonitor(t *testing.T) {
    t.Run("with_all_required_parameters", func(t *testing.T) {
        customClient := &http.Client{
            Timeout: 30 * time.Second,
        }

        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL("https://example.com/metrics"),
            metrics.WithHTTPClient(customClient),
            metrics.WithServiceAndEnvAndRegion("test-service", "test", "us-west-2"),
            metrics.WithPushInterval(10*time.Second),
            metrics.WithProcessMetrics(true),
            metrics.WithDisableCompression(true),
        )

        assert.NoError(t, err, "Expected no error when creating monitor with all required parameters")
        assert.NotNil(t, monitor, "Expected monitor to be created")
    })

    t.Run("missing_storage_url", func(t *testing.T) {
        customClient := &http.Client{
            Timeout: 30 * time.Second,
        }

        monitor, err := metrics.NewVMMetricsMonitor(
            // Missing storage URL
            metrics.WithHTTPClient(customClient),
            metrics.WithServiceAndEnvAndRegion("test-service", "test", "us-west-2"),
        )

        assert.Error(t, err, "Expected error when missing storage URL")
        assert.Nil(t, monitor, "Expected monitor to be nil when validation fails")
        assert.Contains(t, err.Error(), "storageURL is required", "Expected error to mention missing storage URL")
    })

    t.Run("missing_http_client", func(t *testing.T) {
        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL("https://example.com/metrics"),
            // Missing HTTP client
            metrics.WithServiceAndEnvAndRegion("test-service", "test", "us-west-2"),
        )

        assert.Error(t, err, "Expected error when missing HTTP client")
        assert.Nil(t, monitor, "Expected monitor to be nil when validation fails")
        assert.Contains(t, err.Error(), "httpClient is required", "Expected error to mention missing HTTP client")
    })

    t.Run("missing_required_labels", func(t *testing.T) {
        customClient := &http.Client{
            Timeout: 30 * time.Second,
        }

        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL("https://example.com/metrics"),
            metrics.WithHTTPClient(customClient),
            // Missing service, env, region labels
        )

        assert.Error(t, err, "Expected error when missing required labels")
        assert.Nil(t, monitor, "Expected monitor to be nil when validation fails")
        assert.Contains(t, err.Error(), "service name is required", "Expected error to mention missing service name")
    })

    t.Run("partial_labels", func(t *testing.T) {
        customClient := &http.Client{
            Timeout: 30 * time.Second,
        }

        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL("https://example.com/metrics"),
            metrics.WithHTTPClient(customClient),
            metrics.WithExtraLabel("service", "test-service"),
            // Missing env and region labels
        )

        assert.Error(t, err, "Expected error when missing some required labels")
        assert.Nil(t, monitor, "Expected monitor to be nil when validation fails")
        assert.Contains(t, err.Error(), "environment is required", "Expected error to mention missing environment")
    })

    t.Run("default_push_interval", func(t *testing.T) {
        customClient := &http.Client{
            Timeout: 30 * time.Second,
        }

        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL("https://example.com/metrics"),
            metrics.WithHTTPClient(customClient),
            metrics.WithServiceAndEnvAndRegion("test-service", "test", "us-west-2"),
            // No push interval specified - should use default
        )

        assert.NoError(t, err, "Expected no error when using default push interval")
        assert.NotNil(t, monitor, "Expected monitor to be created with default push interval")
    })
}

func TestMonitor_Start(t *testing.T) {
    t.Run("successful_start", func(t *testing.T) {
        var receivedRequest bool
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            receivedRequest = true
            w.WriteHeader(http.StatusOK)
        }))
        defer server.Close()

        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL(server.URL),
            metrics.WithHTTPClient(server.Client()),
            metrics.WithServiceAndEnvAndRegion("test-service", "test", "us-west-2"),
            metrics.WithPushInterval(10*time.Millisecond),
            metrics.WithProcessMetrics(false),
            metrics.WithDisableCompression(true),
        )
        assert.NoError(t, err, "Failed to create monitor")

        ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
        defer cancel()

        err = monitor.Start(ctx)
        assert.NoError(t, err, "Expected no error when starting monitor")

        time.Sleep(30 * time.Millisecond)

        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
        defer shutdownCancel()

        err = monitor.Shutdown(shutdownCtx)
        assert.NoError(t, err, "Expected no error when shutting down monitor")

        assert.True(t, receivedRequest, "Expected metrics server to receive a request")
    })

    t.Run("invalid_url", func(t *testing.T) {
        monitor, err := metrics.NewVMMetricsMonitor(
            metrics.WithStorageURL("invalid://url"),
            metrics.WithHTTPClient(&http.Client{}),
            metrics.WithServiceAndEnvAndRegion("test-service", "test", "us-west-2"),
            metrics.WithPushInterval(10*time.Millisecond),
        )
        assert.NoError(t, err, "Failed to create monitor")

        ctx := context.Background()
        err = monitor.Start(ctx)

        assert.Error(t, err, "Expected error when starting monitor with invalid URL")
        assert.Contains(t, err.Error(), "failed to initialize metrics push", "Expected error to mention metrics push initialization failure")
    })
}

