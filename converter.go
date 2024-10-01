package slogdatadog

import (
	"reflect"

	"log/slog"

	slogcommon "github.com/samber/slog-common"
)

var SourceKey = "source"
var ErrorKeys = []string{"error", "err"}

type Converter func(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record) map[string]any

func DefaultConverter(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record) map[string]any {
	// aggregate all attributes
	attrs := slogcommon.AppendRecordAttrsToAttrs(loggerAttr, groups, record)

	// developer formatters
	if addSource {
		attrs = append(attrs, slogcommon.Source(SourceKey, record))
	}
	attrs = slogcommon.ReplaceAttrs(replaceAttr, []string{}, attrs...)
	attrs = slogcommon.RemoveEmptyAttrs(attrs)

	// handler formatter
	log := map[string]any{
		"@timestamp":     record.Time.UTC(),
		"logger.name":    name,
		"logger.version": version,
		"level":          record.Level.String(),
		"message":        record.Message,
	}

	attrToDatadogLog("", attrs, &log)
	return log
}

func attrToDatadogLog(base string, attrs []slog.Attr, log *map[string]any) {
	for i := range attrs {
		attr := attrs[i]
		k := attr.Key
		v := attr.Value
		kind := attr.Value.Kind()

		for _, errorKey := range ErrorKeys {
			if attr.Key == errorKey && kind == slog.KindAny {
				if err, ok := attr.Value.Any().(error); ok {
					kind, message, stack := buildExceptions(err)
					(*log)[base+k+".kind"] = kind
					(*log)[base+k+".message"] = message
					(*log)[base+k+".stack"] = stack
				} else {
					attrToDatadogLog(base+k+".", v.Group(), log)
				}
			}
		}

		if attr.Key == "user" && kind == slog.KindGroup {
			attrToDatadogLog("usr.", v.Group(), log)
		} else {
			switch kind {
			case slog.KindGroup:
				attrToDatadogLog(base+k+".", v.Group(), log)
			case slog.KindBool:
				(*log)[base+k] = v.Bool()
			case slog.KindFloat64:
				(*log)[base+k] = v.Float64()
			case slog.KindInt64:
				(*log)[base+k] = v.Int64()
			case slog.KindString:
				(*log)[base+k] = v.String()
			case slog.KindAny:
				(*log)[base+k] = v.Any()
			default:
				(*log)[base+k] = slogcommon.ValueToString(v)
			}
		}
	}
}

func buildExceptions(err error) (kind string, message string, stack string) {
	return reflect.TypeOf(err).String(), err.Error(), ""
}
