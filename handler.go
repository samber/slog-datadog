package slogdatadog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	// Batching bool	// @TODO

	// source parameters
	Service    string
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

	return &DatadogHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
	}
}

var _ slog.Handler = (*DatadogHandler)(nil)

type DatadogHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
}

func (h *DatadogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.option.Level.Level()
}

func (h *DatadogHandler) Handle(ctx context.Context, record slog.Record) error {
	fromContext := slogcommon.ContextExtractor(ctx, h.option.AttrFromContext)
	log := h.option.Converter(h.option.AddSource, h.option.ReplaceAttr, append(h.attrs, fromContext...), h.groups, &record)

	bytes, err := h.option.Marshaler(log)
	if err != nil {
		return err
	}

	// non-blocking
	go func() {
		// @TODO: batching
		_ = h.send(string(bytes))
	}()

	return nil
}

func (h *DatadogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DatadogHandler{
		option: h.option,
		attrs:  slogcommon.AppendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
	}
}

func (h *DatadogHandler) WithGroup(name string) slog.Handler {
	return &DatadogHandler{
		option: h.option,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
}

func (h *DatadogHandler) send(message string) error {
	var tags []string
	if h.option.GlobalTags != nil {
		for key, value := range h.option.GlobalTags {
			tags = append(tags, fmt.Sprintf("%v:%v", key, value))
		}
	}

	body := []datadogV2.HTTPLogItem{
		{
			Ddsource: datadog.PtrString(name),
			Hostname: datadog.PtrString(h.option.Hostname),
			Service:  datadog.PtrString(h.option.Service),
			Ddtags:   datadog.PtrString(strings.Join(tags, ",")),
			Message:  message,
		},
	}

	ctx, cancel := context.WithTimeout(h.option.Context, h.option.Timeout)
	defer cancel()

	api := datadogV2.NewLogsApi(h.option.Client)
	_, _, err := api.SubmitLog(ctx, body, *datadogV2.NewSubmitLogOptionalParameters().WithContentEncoding(datadogV2.CONTENTENCODING_DEFLATE))
	return err
}
