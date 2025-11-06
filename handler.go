package slogdatadog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"log/slog"

	slogcommon "github.com/samber/slog-common"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

type Option struct {
	// log level (default: debug)
	Level slog.Leveler

	// datadog endpoint
	Client  *datadog.APIClient
	Context context.Context
	Timeout time.Duration // default: 10s

	// batching (default: disabled)
	Batching      bool
	BatchDuration time.Duration // default: 5s
	MaxBatchSize  int           // default: 0 (no limit)

	// source parameters
	Service string
	// source (optional): Allows overriding the `source` field sent to DD. defaulted to version.name
	Source     string
	Hostname   string
	GlobalTags map[string]string

	// optional: customize Datadog message builder
	Converter Converter
	// optional: custom marshaler
	Marshaler func(v any) ([]byte, error)
	// optional: fetch attributes from context
	AttrFromContext []func(ctx context.Context) []slog.Attr

	// optional: see slog.HandlerOptions
	AddSource   bool
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

func (o Option) NewDatadogHandler() slog.Handler {
	if o.Level == nil {
		o.Level = slog.LevelDebug
	}

	if o.Client == nil {
		panic("missing Datadog client")
	}

	if o.Context == nil {
		o.Context = context.Background()
	}

	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}

	if o.Converter == nil {
		o.Converter = DefaultConverter
	}

	if o.Marshaler == nil {
		o.Marshaler = json.Marshal
	}

	if o.AttrFromContext == nil {
		o.AttrFromContext = []func(ctx context.Context) []slog.Attr{}
	}

	if o.BatchDuration == 0 {
		o.BatchDuration = 5 * time.Second
	}

	handler := &DatadogHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
	}

	// Start the buffer processing goroutine if batching is enabled
	if o.Batching {
		handler.batch = &batchState{
			bufferTimer: time.NewTimer(o.BatchDuration),
		}
		go handler.processBuffer()
	}

	return handler
}

var _ slog.Handler = (*DatadogHandler)(nil)

type batchState struct {
	buffer                  []string
	immediateFlushScheduled bool
	bufferMu                sync.Mutex
	bufferTimer             *time.Timer
	bufferTimerMu           sync.Mutex
}

type DatadogHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
	batch  *batchState // nil when batching is disabled
}

func (h *DatadogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.option.Level.Level()
}

func (h *DatadogHandler) Handle(ctx context.Context, record slog.Record) error {
	bytes, err := handle(h, ctx, record)
	if err != nil {
		return err
	}

	if h.option.Batching {
		h.batch.bufferMu.Lock()
		defer h.batch.bufferMu.Unlock()

		h.batch.buffer = append(h.batch.buffer, string(bytes))
		if h.option.MaxBatchSize > 0 &&
			len(h.batch.buffer) >= h.option.MaxBatchSize &&
			!h.batch.immediateFlushScheduled {
			// if the buffer is full, schedule a flush immediately
			h.batch.immediateFlushScheduled = true
			h.scheduleFlush(0)
		}
		return nil
	}

	// non-blocking
	go func() {
		_ = h.send([]string{string(bytes)})
	}()

	return nil
}

func handle(h *DatadogHandler, ctx context.Context, record slog.Record) ([]byte, error) {
	fromContext := slogcommon.ContextExtractor(ctx, h.option.AttrFromContext)
	log := h.option.Converter(h.option.AddSource, h.option.ReplaceAttr, append(h.attrs, fromContext...), h.groups, &record)

	return h.option.Marshaler(log)
}

func (h *DatadogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DatadogHandler{
		option: h.option,
		attrs:  slogcommon.AppendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
		batch:  h.batch, // Share batching state with parent handler
	}
}

func (h *DatadogHandler) WithGroup(name string) slog.Handler {
	// https://cs.opensource.google/go/x/exp/+/46b07846:slog/handler.go;l=247
	if name == "" {
		return h
	}

	return &DatadogHandler{
		option: h.option,
		attrs:  h.attrs,
		groups: append(h.groups, name),
		batch:  h.batch, // Share batching state with parent handler
	}
}

func (h *DatadogHandler) Flush(ctx context.Context) error {
	errChan := make(chan error)
	go func() {
		errChan <- h.flushBuffer()
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *DatadogHandler) flushBuffer() error {
	h.batch.bufferMu.Lock()
	messages := h.batch.buffer
	h.batch.buffer = nil
	h.batch.immediateFlushScheduled = false
	h.batch.bufferMu.Unlock()

	if len(messages) == 0 {
		return nil
	}

	return h.send(messages)
}

func (h *DatadogHandler) stopBufferTimer() {
	h.batch.bufferTimerMu.Lock()
	defer h.batch.bufferTimerMu.Unlock()

	// Stop the timer if it's running
	if !h.batch.bufferTimer.Stop() {
		// Drain stale value if timer fired before stopped
		<-h.batch.bufferTimer.C
	}
}

func (h *DatadogHandler) scheduleFlush(duration time.Duration) {
	h.batch.bufferTimerMu.Lock()
	defer h.batch.bufferTimerMu.Unlock()

	// Stop the timer if it's running
	if !h.batch.bufferTimer.Stop() {
		// Drain stale value if timer fired before stopped
		// This is a non-blocking read in case processBuffer reads the channel here.
		select {
		case <-h.batch.bufferTimer.C:
		default:
		}
	}

	// Reset the timer to the new duration
	h.batch.bufferTimer.Reset(duration)
}

func (h *DatadogHandler) processBuffer() {
	for {
		select {
		case <-h.option.Context.Done():
			_ = h.flushBuffer()
			h.stopBufferTimer()
			return
		case <-h.batch.bufferTimer.C:
			_ = h.flushBuffer()

			// Check if buffer filled up again during the flush
			// If immediateFlushScheduled is true, Handle() already scheduled a flush, so don't schedule again
			// Otherwise, schedule based on buffer size
			h.batch.bufferMu.Lock()
			if !h.batch.immediateFlushScheduled {
				if h.option.MaxBatchSize > 0 && len(h.batch.buffer) >= h.option.MaxBatchSize {
					// Buffer is still full, flush immediately
					h.scheduleFlush(0)
					h.batch.immediateFlushScheduled = true
				} else {
					// Buffer is below threshold, use normal periodic duration
					h.scheduleFlush(h.option.BatchDuration)
				}
			}
			h.batch.bufferMu.Unlock()

		}
	}
}

func (h *DatadogHandler) send(messages []string) error {
	var tags []string
	if h.option.GlobalTags != nil {
		for key, value := range h.option.GlobalTags {
			tags = append(tags, fmt.Sprintf("%v:%v", key, value))
		}
	}

	source := h.option.Source
	if source == "" {
		source = name
	}

	body := make([]datadogV2.HTTPLogItem, len(messages))
	for i, message := range messages {
		body[i] = datadogV2.HTTPLogItem{
			Ddsource: datadog.PtrString(source),
			Hostname: datadog.PtrString(h.option.Hostname),
			Service:  datadog.PtrString(h.option.Service),
			Ddtags:   datadog.PtrString(strings.Join(tags, ",")),
			Message:  message,
		}
	}

	ctx, cancel := context.WithTimeout(h.option.Context, h.option.Timeout)
	defer cancel()

	api := datadogV2.NewLogsApi(h.option.Client)
	_, _, err := api.SubmitLog(ctx, body, *datadogV2.NewSubmitLogOptionalParameters().WithContentEncoding(datadogV2.CONTENTENCODING_DEFLATE))
	return err
}
