package metrics_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chou-godaddy/vmmetrics"
)

// This example demonstrates how to set up a VMMetricsMonitor for monitoring application metrics.
func ExampleVMMetricsMonitor() {
	// Create an HTTP client
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Create a new VMMetricsMonitor with required options
	monitor, err := metrics.NewVMMetricsMonitor(
		metrics.WithStorageURL("http://victoria-metrics:8428/api/v1/import/prometheus"),
		metrics.WithHTTPClient(httpClient),
		metrics.WithServiceAndEnvAndRegion("my-service", "production", "us-west-2"),
		metrics.WithPushInterval(30*time.Second),
		metrics.WithProcessMetrics(true),
	)
	if err != nil {
		fmt.Printf("Failed to create metrics monitor: %v\n", err)
		return
	}

	// Create context that will be canceled on SIGINT or SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("Received shutdown signal")
		cancel()
	}()

	// Start the metrics monitoring
	fmt.Println("Starting metrics monitor...")
	if err := monitor.Start(ctx); err != nil {
		fmt.Printf("Failed to start metrics monitor: %v\n", err)
		return
	}
	fmt.Println("Metrics monitor started successfully")

	// Create some metrics that will be captured
	counter := metrics.NewCounter("api_requests_total")
	gauge := metrics.NewGauge("active_connections", nil)
	histogram := metrics.NewHistogram("request_duration_seconds")

	// Simulate some metrics updates
	counter.Inc()
	gauge.Set(42)
	histogram.Update(0.25)

	// Wait for context cancellation in a real application
	// This would run until receiving a shutdown signal
	fmt.Println("Monitoring metrics (would run until shutdown in a real application)")

	// For example purposes, simulate a shutdown after 1 second
	time.Sleep(1 * time.Second)
	cancel()
	fmt.Println("Shutting down metrics monitor...")

	// Perform graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := monitor.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("Error during metrics monitor shutdown: %v\n", err)
		return
	}
	fmt.Println("Metrics monitor shut down successfully")
}

// This example demonstrates how to set up a monitor with TLS client for secure metrics transmission for applications running in AWS.
func ExampleVMMetricsMonitor_withTLS() {
	// Create a TLS-enabled HTTP client
	tlsClient, err := metrics.WithTLSConfigClient(
		"cert-secret-name",
		"key-secret-name",
		false,
		"us-west-2",
	)
	if err != nil {
		fmt.Printf("Failed to create TLS client: %v\n", err)
		return
	}

	// Create a monitor with the TLS client
	monitor, err := metrics.NewVMMetricsMonitor(
		metrics.WithStorageURL("https://secure-victoria-metrics:8428/api/v1/import/prometheus"),
		metrics.WithHTTPClient(tlsClient),
		metrics.WithServiceAndEnvAndRegion("secure-service", "production", "us-west-2"),
	)
	if err != nil {
		fmt.Printf("Failed to create secure metrics monitor: %v\n", err)
		return
	}

	// Start the metrics monitoring
	fmt.Println("Starting secure metrics monitor...")
	ctx := context.Background()
	if err := monitor.Start(ctx); err != nil {
		fmt.Printf("Failed to start secure metrics monitor: %v\n", err)
		return
	}
	fmt.Println("Secure metrics monitor started successfully")
}
