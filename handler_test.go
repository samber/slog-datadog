package slogdatadog

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"testing"

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
		t.Errorf("handle() error = %v", err)
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
		t.Errorf("Expected\n  %v\nGot\n  %v", expected, data)
	}
}
