package slogdatadog

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"

	"golang.org/x/exp/slog"
)

type Converter func(loggerAttr []slog.Attr, record *slog.Record) map[string]any

func DefaultConverter(loggerAttr []slog.Attr, record *slog.Record) map[string]any {
	log := map[string]any{}

	log["logger.name"] = name
	log["logger.version"] = version

	attrToDatadogLog("", loggerAttr, &log)
	record.Attrs(func(attr slog.Attr) bool {
		attrToDatadogLog("", []slog.Attr{attr}, &log)
		return true
	})

	return log
}

func attrToDatadogLog(base string, attrs []slog.Attr, log *map[string]any) {
	for i := range attrs {
		attr := attrs[i]
		k := attr.Key
		v := attr.Value
		kind := attr.Value.Kind()

		if attr.Key == "error" && kind == slog.KindAny {
			if err, ok := attr.Value.Any().(error); ok {
				kind, message, stack := buildExceptions(err)
				(*log)[base+k+".kind"] = kind
				(*log)[base+k+".message"] = message
				(*log)[base+k+".stack"] = stack
			} else {
				attrToDatadogLog(base+k+".", v.Group(), log)
			}
		} else if attr.Key == "user" && kind == slog.KindGroup {
			attrToDatadogLog("usr.", v.Group(), log)
		} else if kind == slog.KindGroup {
			attrToDatadogLog(base+k+".", v.Group(), log)
		} else {
			(*log)[base+k] = attrToValue(v)
		}
	}
}

func attrToValue(v slog.Value) string {
	kind := v.Kind()

	switch kind {
	case slog.KindAny:
		return anyValueToString(v)
	case slog.KindLogValuer:
		return anyValueToString(v)
	case slog.KindGroup:
		// not expected to reach this line
		return anyValueToString(v)
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%d", v.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%f", v.Float64())
	case slog.KindString:
		return v.String()
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().String()
	default:
		return anyValueToString(v)
	}
}

func anyValueToString(v slog.Value) string {
	if tm, ok := v.Any().(encoding.TextMarshaler); ok {
		data, err := tm.MarshalText()
		if err != nil {
			return ""
		}

		return string(data)
	}

	return fmt.Sprintf("%+v", v.Any())
}

func buildExceptions(err error) (kind string, message string, stack string) {
	return reflect.TypeOf(err).String(), err.Error(), ""
}
