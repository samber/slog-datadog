package slogdatadog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
	"golang.org/x/exp/slog"
)

type Option struct {
	// log level (default: debug)
	Level slog.Leveler

	// datadog endpoint
	Client  *datadog.APIClient
	Context context.Context
	// batching (default: disabled)
	// Batching bool	// @TODO

	// source parameters
	Service    string
	Hostname   string
	GlobalTags map[string]string

	// optional: customize Datadog message builder
	Converter Converter
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

	return &DatadogHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
	}
}

type DatadogHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
}

func (h *DatadogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.option.Level.Level()
}

func (h *DatadogHandler) Handle(ctx context.Context, record slog.Record) error {
	converter := DefaultConverter
	if h.option.Converter != nil {
		converter = h.option.Converter
	}

	log := converter(h.attrs, &record)
	bytes, err := json.Marshal(log)
	if err != nil {
		return err
	}

	// @TODO: batching
	return h.send(string(bytes))
}

func (h *DatadogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DatadogHandler{
		option: h.option,
		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs),
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
			Ddsource: datadog.PtrString("samber/slog-datadog"),
			Hostname: datadog.PtrString(h.option.Hostname),
			Service:  datadog.PtrString(h.option.Service),
			Ddtags:   datadog.PtrString(strings.Join(tags, ",")),
			Message:  message,
		},
	}

	api := datadogV2.NewLogsApi(h.option.Client)
	_, _, err := api.SubmitLog(h.option.Context, body, *datadogV2.NewSubmitLogOptionalParameters().WithContentEncoding(datadogV2.CONTENTENCODING_DEFLATE))
	return err
}
