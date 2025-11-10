package slogdatadog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
)

func TestArgFormatting(t *testing.T) {
	type ExampleStruct struct {
		ExampleField1 string
		ExampleField2 string
	}

	var r slog.Record
	r.Add(
		"Attr1", slog.AnyValue(ExampleStruct{ExampleField1: "foo", ExampleField2: "bar"}),
		"Attr2", slog.StringValue("foo"),
		"Attr3", slog.IntValue(999),
		"Attr4", slog.Float64Value(999.99),
	)
	r.Message = "test"

	h := Option{
		Client: &datadog.APIClient{},
	}.NewDatadogHandler()
	ddh := h.(*DatadogHandler)
	bytes, err := handle(ddh, context.Background(), r)

	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}

	var data map[string]any
	err = json.Unmarshal(bytes, &data)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]any{
		"@timestamp":     "0001-01-01T00:00:00Z",
		"level":          "INFO",
		"logger.name":    "samber/slog-datadog",
		"logger.version": "VERSION", // won't be replaced in test build
		"Attr1":          map[string]any{"ExampleField1": "foo", "ExampleField2": "bar"},
		"Attr2":          "foo",
		"Attr3":          999.0,
		"Attr4":          999.99,
		"message":        "test",
	}

	if !reflect.DeepEqual(expected, data) {
		t.Fatalf("Expected\n  %v\nGot\n  %v", expected, data)
	}
}

// createTestClient creates a test Datadog API client with proper configuration
// Returns the client and a cleanup function that should be called with defer
func createTestClient() (*datadog.APIClient, func()) {
	// Create a custom HTTP client with a configurable transport
	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   true, // Disable keep-alives to prevent goroutine leaks in tests
		MaxIdleConnsPerHost: -1,   // No limit
	}

	httpClient := &http.Client{
		Transport: transport,
	}

	config := datadog.NewConfiguration()
	config.HTTPClient = httpClient
	client := datadog.NewAPIClient(config)

	cleanup := func() {
		transport.CloseIdleConnections()
	}

	return client, cleanup
}

func TestBatchingDisabled(t *testing.T) {
	ctx := context.Background()
	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:   client,
		Context:  ctx,
		Batching: false,
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)

	// Log some messages
	logger := slog.New(handler)
	logger.Info("message 1")
	logger.Info("message 2")
	logger.Info("message 3")

	// When batching is disabled, batch state is nil (not allocated)
	if ddh.batch != nil {
		t.Errorf("Expected batch to be nil when batching is disabled")
	}
}

func TestBatchingEnabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	testCases := []struct {
		name         string
		createLogger func(base slog.Handler) *slog.Logger
	}{
		{
			name: "parent handler",
			createLogger: func(base slog.Handler) *slog.Logger {
				return slog.New(base)
			},
		},
		{
			name: "WithAttrs handler",
			createLogger: func(base slog.Handler) *slog.Logger {
				return slog.New(base.WithAttrs([]slog.Attr{slog.String("test", "value")}))
			},
		},
		{
			name: "WithGroup handler",
			createLogger: func(base slog.Handler) *slog.Logger {
				return slog.New(base.WithGroup("group"))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := Option{
				Client:        client,
				Context:       ctx,
				Batching:      true,
				BatchDuration: 500 * time.Millisecond,
			}.NewDatadogHandler()

			ddh := handler.(*DatadogHandler)

			// Create logger using the test case's factory
			logger := tc.createLogger(handler)

			// Log messages
			for i := range 3 {
				logger.Info("message", "index", i)
			}

			// Verify messages are buffered
			ddh.batch.bufferMu.Lock()
			bufferLen := len(ddh.batch.buffer)
			ddh.batch.bufferMu.Unlock()

			if bufferLen != 3 {
				t.Errorf("Expected 3 messages in buffer, got %d", bufferLen)
			}

			// Wait for batch duration to trigger flush
			time.Sleep(600 * time.Millisecond)

			// Buffer should be empty after flush
			ddh.batch.bufferMu.Lock()
			bufferLen = len(ddh.batch.buffer)
			ddh.batch.bufferMu.Unlock()

			if bufferLen != 0 {
				t.Errorf("Expected buffer to be empty after flush, got %d items", bufferLen)
			}
		})
	}
}

func TestBatchingMaxBatchSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	maxBatchSize := 5
	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration to ensure MaxBatchSize triggers first
		MaxBatchSize:  maxBatchSize,
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)

	// Log exactly maxBatchSize messages
	logger := slog.New(handler)
	for i := range maxBatchSize {
		logger.Info("message", "index", i)
	}

	// Give some time for the flush to trigger
	time.Sleep(100 * time.Millisecond)

	// Buffer should be empty after flush
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after reaching MaxBatchSize, got %d items", bufferLen)
	}

	// Log more messages
	for i := range 3 {
		logger.Info("message after flush", "index", i)
	}

	// Buffer should have the new messages
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 3 {
		t.Errorf("Expected 3 messages in buffer after MaxBatchSize flush, got %d", bufferLen)
	}
}

func TestBatchingContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration to ensure context cancellation triggers first
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)

	// Log some messages
	logger := slog.New(handler)
	logger.Info("message 1")
	logger.Info("message 2")
	logger.Info("message 3")

	// Verify messages are buffered
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 3 {
		t.Errorf("Expected 3 messages in buffer, got %d", bufferLen)
	}

	// Cancel context to trigger flush
	cancel()

	// Give some time for the flush to complete
	time.Sleep(100 * time.Millisecond)

	// Buffer should be empty after flush
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after context cancellation, got %d items", bufferLen)
	}
}

func TestBatchingManualFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration to ensure manual flush is used
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)

	// Log some messages
	logger := slog.New(handler)
	logger.Info("message 1")
	logger.Info("message 2")
	logger.Info("message 3")

	// Verify messages are buffered
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 3 {
		t.Errorf("Expected 3 messages in buffer, got %d", bufferLen)
	}

	// Manually flush
	flushCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_ = ddh.Flush(flushCtx)
	// Note: Flush() will return an error (401 Unauthorized) since we don't have valid credentials
	// but we're only testing the batching behavior, not the actual send to Datadog

	// Buffer should be empty after flush (regardless of send error)
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after manual flush, got %d items", bufferLen)
	}
}

func TestBatchingMultipleBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration, use manual flush
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)
	logger := slog.New(handler)

	// Log first batch
	logger.Info("batch 1 message 1")
	logger.Info("batch 1 message 2")

	// Verify first batch is buffered
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 2 {
		t.Errorf("Expected 2 messages in first batch, got %d", bufferLen)
	}

	// Manually flush first batch
	flushCtx1, cancel1 := context.WithTimeout(ctx, 1*time.Second)
	_ = ddh.Flush(flushCtx1)
	cancel1()

	// Buffer should be empty
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after first flush, got %d items", bufferLen)
	}

	// Log second batch
	logger.Info("batch 2 message 1")
	logger.Info("batch 2 message 2")
	logger.Info("batch 2 message 3")

	// Verify second batch is buffered
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 3 {
		t.Errorf("Expected 3 messages in buffer for second batch, got %d", bufferLen)
	}

	// Manually flush second batch
	flushCtx2, cancel2 := context.WithTimeout(ctx, 1*time.Second)
	_ = ddh.Flush(flushCtx2)
	cancel2()

	// Buffer should be empty again
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after second flush, got %d items", bufferLen)
	}
}

func TestBatchingDefaultDuration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:   client,
		Context:  ctx,
		Batching: true,
		// BatchDuration not set, should default to 5 seconds
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)

	expectedDuration := 5 * time.Second
	if ddh.option.BatchDuration != expectedDuration {
		t.Errorf("Expected default BatchDuration to be %v, got %v", expectedDuration, ddh.option.BatchDuration)
	}
}

func TestBatchingConcurrentWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 100 * time.Millisecond,
		MaxBatchSize:  50,
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)
	logger := slog.New(handler)

	// Spawn multiple goroutines writing concurrently
	const numGoroutines = 10
	const messagesPerGoroutine = 100
	done := make(chan bool, numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			for j := range messagesPerGoroutine {
				logger.Info("concurrent message", "goroutine", id, "msg", j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Manually flush to clear remaining items
	flushCtx, flushCancel := context.WithTimeout(ctx, 1*time.Second)
	_ = ddh.Flush(flushCtx)
	flushCancel()

	// Give time for flush to complete
	time.Sleep(100 * time.Millisecond)

	// Verify no panic occurred and buffer is empty after final flush
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	// Buffer should be empty after manual flush
	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after final flush, got %d items", bufferLen)
	}
}

func TestBatchingConcurrentFlushes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)
	logger := slog.New(handler)

	// Add some messages
	for i := range 50 {
		logger.Info("message", "index", i)
	}

	// Call Flush concurrently from multiple goroutines
	const numFlushes = 5
	done := make(chan error, numFlushes)

	for range numFlushes {
		go func() {
			flushCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()
			done <- ddh.Flush(flushCtx)
		}()
	}

	// Wait for all flushes to complete
	for range numFlushes {
		<-done
		// We ignore errors since we don't have valid credentials
	}

	// Verify no panic occurred and buffer is empty
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after concurrent flushes, got %d items", bufferLen)
	}
}

// TestBatchingRaceCondition attempts to trigger race conditions that existed in the original
// implementation where scheduleFlush(0) could deadlock with processBuffer reading from the timer channel.
// This test rapidly fills batches to trigger MaxBatchSize flushes while the timer is also firing.
func TestBatchingRaceCondition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 50 * time.Millisecond, // Short duration to trigger timer frequently
		MaxBatchSize:  5,                      // Small batch size
	}.NewDatadogHandler()

	logger := slog.New(handler)

	// Rapidly fill batches to trigger MaxBatchSize flushes
	// This will cause scheduleFlush(0) to be called while processBuffer
	// might also be reading from the timer channel
	done := make(chan bool)
	go func() {
		for i := range 100 {
			for j := range 5 {
				logger.Info("message", "batch", i, "msg", j)
			}
			// Small delay to let timer fire
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - likely deadlock in scheduleFlush")
	}
}

// TestBatchingOverflowAfterScheduledFlush tests that when MaxBatchSize is reached multiple times,
// flushes continue to be triggered. This ensures that if >= comparison is used (not ==),
// and the bufferFlushScheduled flag prevents duplicate flush scheduling,
// the buffer continues to flush even when it exceeds MaxBatchSize.
//
// Scenario:
// - MaxBatchSize = 5
// - Add 5 messages -> should trigger flush (buffer >= 5 && !bufferFlushScheduled)
// - Flush is scheduled, flag is set
// - Add 10 more messages rapidly before flush completes -> flag prevents duplicate scheduling
// - Flush completes, flag is cleared
// - Next message that reaches >= MaxBatchSize triggers new flush
//
// Without >= comparison or the bufferFlushScheduled flag, messages could accumulate
// beyond MaxBatchSize without triggering additional flushes.
func TestBatchingOverflowAfterScheduledFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, cleanup := createTestClient()
	defer cleanup()

	maxBatchSize := 5
	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration to ensure MaxBatchSize triggers, not timer
		MaxBatchSize:  maxBatchSize,
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)
	logger := slog.New(handler)

	// Test multiple batches in rapid succession with small delays
	// This creates a scenario where messages accumulate after flush is scheduled
	// but before it executes. With >= comparison and immediateFlushScheduled flag,
	// flushes continue to trigger as buffer refills after each flush.
	const totalMessages = 50

	for i := range totalMessages {
		logger.Info("message", "index", i)

		// Small delay every 10 messages to allow some flushes to complete
		// This simulates ongoing message arrival rather than a burst
		if (i+1)%10 == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Wait for all scheduled flushes to complete
	// scheduleFlush(0) resets timer to fire immediately but doesn't guarantee instant execution
	time.Sleep(500 * time.Millisecond)

	// Check buffer - should be empty or very small after all flushes complete
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	// With >= comparison and proper flag handling, buffer should stay small
	// as flushes trigger regularly. Without it, buffer would accumulate.
	if bufferLen > maxBatchSize*2 {
		t.Errorf("Buffer accumulated too many messages: %d (MaxBatchSize=%d). "+
			"This suggests flushes aren't triggering correctly when buffer >= MaxBatchSize",
			bufferLen, maxBatchSize)
	}

	t.Logf("Final buffer size: %d messages (sent %d messages total with MaxBatchSize=%d)",
		bufferLen, totalMessages, maxBatchSize)
}
func TestStop(t *testing.T) {
	ctx := context.Background()
	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:        client,
		Context:       ctx,
		Batching:      true,
		BatchDuration: 10 * time.Second, // Long duration so Stop() handles the flush
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)
	logger := slog.New(handler)

	// Log some messages
	logger.Info("message 1")
	logger.Info("message 2")
	logger.Info("message 3")

	// Verify messages are buffered
	ddh.batch.bufferMu.Lock()
	bufferLen := len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 3 {
		t.Errorf("Expected 3 messages in buffer before Stop(), got %d", bufferLen)
	}

	// Call Stop with timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = ddh.Stop(stopCtx)

	// Buffer should be empty after Stop()
	ddh.batch.bufferMu.Lock()
	bufferLen = len(ddh.batch.buffer)
	ddh.batch.bufferMu.Unlock()

	if bufferLen != 0 {
		t.Errorf("Expected buffer to be empty after Stop(), got %d items", bufferLen)
	}
}

func TestStopWithoutBatching(t *testing.T) {
	ctx := context.Background()
	client, cleanup := createTestClient()
	defer cleanup()

	handler := Option{
		Client:   client,
		Context:  ctx,
		Batching: false,
	}.NewDatadogHandler()

	ddh := handler.(*DatadogHandler)
	logger := slog.New(handler)

	// Log some messages
	logger.Info("message 1")
	logger.Info("message 2")

	// Call Stop - should complete without error even though batching is disabled
	stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := ddh.Stop(stopCtx)

	if err != nil {
		t.Errorf("Expected no error from Stop() when batching is disabled, got %v", err)
	}
}

