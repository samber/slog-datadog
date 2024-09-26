package slogdatadog

import (
	"log/slog"
	"reflect"
	"testing"
	"time"
)

var testLogTime = time.Now()

func TestDefaultConverter(t *testing.T) {
	type ExampleStruct struct {
		ExampleField1 string
		ExampleField2 string
	}

	type args struct {
		addSource   bool
		replaceAttr func(groups []string, a slog.Attr) slog.Attr
		loggerAttr  []slog.Attr
		groups      []string
		record      *slog.Record
	}

	type testCase struct {
		args     args
		expected map[string]any
	}

	cases := []testCase{
		// Struct values are passed through unstringified.
		{
			args: args{
				false,
				dontReplaceAnyAttrs,
				[]slog.Attr{
					{Key: "Attr1", Value: slog.StringValue("foo")},
					{Key: "Attr2", Value: slog.Float64Value(888.88)},
					{Key: "Attr3", Value: slog.AnyValue(ExampleStruct{"foo", "bar"})},
				},
				nil,
				&slog.Record{Message: "test", Time: testLogTime},
			},
			expected: map[string]any{
				"level":   "INFO",
				"Attr1":   "foo",
				"Attr2":   888.88,
				"Attr3":   ExampleStruct{"foo", "bar"},
				"message": "test",
			},
		},
		// replaceAttr function is called and replaces attributes
		{
			args: args{
				false,
				replaceAttrValueWith("Attr1", "baz"),
				[]slog.Attr{{Key: "Attr1", Value: slog.StringValue("foo")},
					{Key: "Attr2", Value: slog.AnyValue(ExampleStruct{"foo", "bar"})}},
				nil,
				&slog.Record{Message: "test", Time: testLogTime},
			},
			expected: map[string]any{
				"level":   "INFO",
				"Attr1":   "baz",
				"Attr2":   ExampleStruct{"foo", "bar"},
				"message": "test",
			},
		},
	}

	for _, c := range cases {
		r := DefaultConverter(c.args.addSource, c.args.replaceAttr, c.args.loggerAttr, c.args.groups, c.args.record)
		addCommonExpectedAttrs(c.expected)
		if !reflect.DeepEqual(r, c.expected) {
			t.Errorf("Expected\n  %+v\nGot\n  %+v\n", c.expected, r)
		}
	}
}

func addCommonExpectedAttrs(m map[string]any) {
	m["@timestamp"] = testLogTime.UTC()
	m["logger.name"] = "samber/slog-datadog"
	m["logger.version"] = "VERSION" // won't be replaced in test build
}

func replaceAttrValueWith(name, replacement string) func(groups []string, a slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == name {
			a.Value = slog.StringValue(replacement)
		}
		return a
	}
}

func dontReplaceAnyAttrs(groups []string, a slog.Attr) slog.Attr {
	return a
}
