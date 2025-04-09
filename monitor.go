package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	gosecrets "github.com/gdcorp-domains/fulfillment-gosecrets"
)

// VMMetricsMonitor is the interface for monitoring VM metrics.
//
// Implementations of this interface provide functionality for starting and
// shutting down metrics monitoring services that push metrics to VictoriaMetrics.
type VMMetricsMonitor interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Monitor represents the VictoriaMetrics monitoring component.
//
// Monitor implements the VMMetricsMonitor interface and provides functionality
// for pushing metrics to a VictoriaMetrics server at specified intervals.
// It supports various configuration options including custom HTTP clients,
// TLS configurations, and metric labels.
type Monitor struct {
	storageURL         string
	pushInterval       time.Duration
	disableCompression bool
	processMetrics     bool
	extraLabels        []labels
	httpClient         *http.Client
	stopChan           chan struct{}
	wg                 sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}

// NewVMMetricsMonitor creates a new instance of the VictoriaMetrics Monitor.
//
// The function accepts a variadic list of MonitorOption functions which configure
// the monitor. At minimum, the following options must be provided:
//   - A storage URL (using WithStorageURL)
//   - An HTTP client (using either WithHTTPClient or WithTLSConfigClient)
//   - Service name, environment, and region labels (via WithServiceAndEnvAndRegion or WithExtraLabel)
//
// If the push interval is not specified, it defaults to 15 seconds.
func NewVMMetricsMonitor(opts ...MonitorOption) (*Monitor, error) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Monitor{
		stopChan:    make(chan struct{}),
		extraLabels: []labels{},
		ctx:         ctx,
		cancel:      cancel,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Validate the monitor configuration
	if err := m.ValidateMonitor(); err != nil {
		return nil, err
	}

	return m, nil
}

// Start starts the VictoriaMetrics monitoring and pushing.
//
// It begins pushing metrics to the configured storage URL at the configured interval.
// The pushing continues until the provided context is canceled or until Shutdown is called.
func (m *Monitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	log.Printf("[METRICS] Starting VictoriaMetrics monitoring...")

	opts := &PushOptions{
		ExtraLabels:        compileLabels(m.extraLabels),
		DisableCompression: m.disableCompression,
		WaitGroup:          &m.wg,
		CustomClient:       m.httpClient,
	}

	m.ctx = ctx

	err := InitPushWithOptions(ctx, m.storageURL, m.pushInterval, m.processMetrics, opts)
	if err != nil {
		log.Printf("[METRICS] Error initializing push: %v", err)
		return fmt.Errorf("failed to initialize metrics push: %w", err)
	}

	log.Printf("[METRICS] VictoriaMetrics monitoring started successfully with push interval: %s", m.pushInterval)

	return nil
}

// Shutdown gracefully shuts down the VictoriaMetrics Monitor.
//
// It stops pushing when the provided context is canceled.
func (m *Monitor) Shutdown(ctx context.Context) error {
	log.Print("[METRICS] Initiating shutdown of VictoriaMetrics Monitor...")

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Print("[METRICS] VictoriaMetrics Monitor shutdown completed successfully.")
	case <-ctx.Done():
		log.Printf("[METRICS] VictoriaMetrics Monitor shutdown timed out: %v", ctx.Err())
		return ctx.Err()
	}

	return nil
}

// MonitorOption defines a function that can configure a Monitor.
//
// This follows the functional options pattern for configuring the Monitor instance.
type MonitorOption func(*Monitor)

// WithStorageURL sets the VictoriaMetrics storage URL for the Monitor.
//
// The URL should point to a VictoriaMetrics server endpoint that accepts
// metrics in Prometheus format
func WithStorageURL(url string) MonitorOption {
	return func(m *Monitor) {
		m.storageURL = url
	}
}

// WithPushInterval sets the interval at which metrics are pushed to the storage URL.
//
// If not specified, the default interval is 15 seconds.
func WithPushInterval(interval time.Duration) MonitorOption {
	return func(m *Monitor) {
		m.pushInterval = interval
	}
}

// WithProcessMetrics enables or disables collection of process metrics.
//
// When enabled, Go runtime metrics and process metrics will be collected and pushed.
func WithProcessMetrics(enable bool) MonitorOption {
	return func(m *Monitor) {
		m.processMetrics = enable
	}
}

// WithDisableCompression disables HTTP request body compression when pushing metrics.
//
// When compression is enabled, it can reduce network bandwidth usage.
func WithDisableCompression(disable bool) MonitorOption {
	return func(m *Monitor) {
		m.disableCompression = disable
	}
}

// WithHTTPClient allows setting a custom HTTP client for the Monitor.
//
// The provided client will be used for pushing metrics to the storage URL.
func WithHTTPClient(client *http.Client) MonitorOption {
	return func(m *Monitor) {
		m.httpClient = client
	}
}

// WithTLSConfigClient creates an HTTP client with TLS configuration.
//
// The certificate and key are retrieved from AWS Secrets Manager using the provided
// names and region. The returned client can be used with WithHTTPClient.
func WithTLSConfigClient(certName string, keyName string, insecureSkipVerify bool, region string) (*http.Client, error) {
	transport := &http.Transport{}

	certificate, err := retrieveCert(certName, keyName, region)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve TLS config: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{*certificate},
		InsecureSkipVerify: insecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	transport.TLSClientConfig = tlsConfig

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	log.Printf("[METRICS] HTTP client for VictoriaMetrics Monitor created")
	return client, nil
}

// WithExtraLabel adds a custom label to the metrics.
//
// Labels are key-value pairs that are added to all metrics pushed by the Monitor.
func WithExtraLabel(key, value string) MonitorOption {
	return func(m *Monitor) {
		m.extraLabels = append(m.extraLabels, labels{key, value})
	}
}

// WithServiceAndEnvAndRegion sets the service name, environment, and region labels.
//
// This is a convenience function for adding the three most common labels that
// are required by the validator.
func WithServiceAndEnvAndRegion(serviceName, env, region string) MonitorOption {
	return func(m *Monitor) {
		WithExtraLabel("service", serviceName)(m)
		WithExtraLabel("env", env)(m)
		WithExtraLabel("region", region)(m)
	}
}

// ValidateMonitor validates that the Monitor has all required fields.
//
// It checks that:
// - A storage URL is provided
// - An HTTP client is provided
// - Service name, environment, and region labels are provided
// - The push interval has a valid value, or sets the default if not provided
func (m *Monitor) ValidateMonitor() error {
	// Check that a storage URL is provided
	if m.storageURL == "" {
		return fmt.Errorf("storageURL is required")
	}

	// Check that an HTTP client is provided
	if m.httpClient == nil {
		return fmt.Errorf("httpClient is required, use either WithHTTPClient or WithTLSConfigClient option")
	}

	// Set default push interval if not provided
	if m.pushInterval <= 0 {
		m.pushInterval = 15 * time.Second
	}

	// Check that service name, environment, region are provided in extraLabels
	var hasService, hasEnv, hasRegion bool
	for _, label := range m.extraLabels {
		if label.key == "service" {
			hasService = true
		}
		if label.key == "env" {
			hasEnv = true
		}
		if label.key == "region" {
			hasRegion = true
		}
	}

	if !hasService {
		return fmt.Errorf("service name is required, use WithExtraLabel(\"service\", \"your-service\")")
	}

	if !hasEnv {
		return fmt.Errorf("environment is required, use WithExtraLabel(\"env\", \"your-env\")")
	}

	if !hasRegion {
		return fmt.Errorf("region is required, use WithExtraLabel(\"region\", \"your-region\")")
	}

	return nil
}

// labels represents a key-value pair for extra labels
type labels struct {
	key string
	val string
}

// String formats the labels as a string
func (l labels) String() string {
	return fmt.Sprintf(`%s="%s"`, l.key, l.val)
}

// compileLabels compiles the extra labels into a string.
//
// The result is a comma-separated list of key-value pairs suitable for
// inclusion in metric labels.
func compileLabels(extraLabels []labels) string {
	var labels []string
	for _, l := range extraLabels {
		labels = append(labels, l.String())
	}
	return strings.Join(labels, ",")
}

// retrieveCert retrieves the TLS certificate and key from AWS Secrets Manager.
//
// The certificate and key are retrieved using the provided names and region.
// Returns a tls.Certificate that can be used with TLS configurations.
func retrieveCert(certName, keyName, region string) (*tls.Certificate, error) {
	secretRetriever := gosecrets.NewSecretRetriever()

	certBytes, certErr := secretRetriever.Get(context.Background(), gosecrets.SecretConfig{
		AWS: &gosecrets.AWSSecretConfig{
			Name:   certName,
			Region: region,
		},
	})
	if certErr != nil {
		return nil, certErr
	}
	keyBytes, keyErr := secretRetriever.Get(context.Background(), gosecrets.SecretConfig{
		AWS: &gosecrets.AWSSecretConfig{
			Name:   keyName,
			Region: region,
		},
	})
	if keyErr != nil {
		return nil, keyErr
	}
	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, err
	}

	return &cert, nil
}
