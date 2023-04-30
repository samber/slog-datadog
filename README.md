
# slog: Datadog handler

[![tag](https://img.shields.io/github/tag/samber/slog-datadog.svg)](https://github.com/samber/slog-datadog/releases)
![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.20.3-%23007d9c)
[![GoDoc](https://godoc.org/github.com/samber/slog-datadog?status.svg)](https://pkg.go.dev/github.com/samber/slog-datadog)
![Build Status](https://github.com/samber/slog-datadog/actions/workflows/test.yml/badge.svg)
[![Go report](https://goreportcard.com/badge/github.com/samber/slog-datadog)](https://goreportcard.com/report/github.com/samber/slog-datadog)
[![Coverage](https://img.shields.io/codecov/c/github/samber/slog-datadog)](https://codecov.io/gh/samber/slog-datadog)
[![Contributors](https://img.shields.io/github/contributors/samber/slog-datadog)](https://github.com/samber/slog-datadog/graphs/contributors)
[![License](https://img.shields.io/github/license/samber/slog-datadog)](./LICENSE)

A [Datadog](https://datadoghq.com) Handler for [slog](https://pkg.go.dev/golang.org/x/exp/slog) Go library.

**See also:**

- [slog-multi](https://github.com/samber/slog-multi): `slog.Handler` chaining, fanout, routing, failover, load balancing...
- [slog-formatter](https://github.com/samber/slog-formatter): `slog` attribute formatting
- [slog-gin](https://github.com/samber/slog-gin): Gin middleware for `slog` logger
- [slog-echo](https://github.com/samber/slog-echo): Echo middleware for `slog` logger
- [slog-fiber](https://github.com/samber/slog-fiber): Fiber middleware for `slog` logger
- [slog-datadog](https://github.com/samber/slog-datadog): A `slog` handler for `Datadog`
- [slog-rollbar](https://github.com/samber/slog-rollbar): A `slog` handler for `Rollbar`
- [slog-sentry](https://github.com/samber/slog-sentry): A `slog` handler for `Sentry`
- [slog-syslog](https://github.com/samber/slog-syslog): A `slog` handler for `Syslog`
- [slog-logstash](https://github.com/samber/slog-logstash): A `slog` handler for `Logstash`
- [slog-fluentd](https://github.com/samber/slog-fluentd): A `slog` handler for `Fluentd`
- [slog-graylog](https://github.com/samber/slog-graylog): A `slog` handler for `Graylog`
- [slog-loki](https://github.com/samber/slog-loki): A `slog` handler for `Loki`
- [slog-slack](https://github.com/samber/slog-slack): A `slog` handler for `Slack`
- [slog-telegram](https://github.com/samber/slog-telegram): A `slog` handler for `Telegram`
- [slog-mattermost](https://github.com/samber/slog-mattermost): A `slog` handler for `Mattermost`
- [slog-microsoft-teams](https://github.com/samber/slog-microsoft-teams): A `slog` handler for `Microsoft Teams`
- [slog-webhook](https://github.com/samber/slog-webhook): A `slog` handler for `Webhook`
- [slog-kafka](https://github.com/samber/slog-kafka): A `slog` handler for `Kafka`

## 🚀 Install

```sh
go get github.com/samber/slog-datadog
```

**Compatibility**: go >= 1.20.3

This library is v0 and follows SemVer strictly. On `slog` final release (go 1.21), this library will go v1.

No breaking changes will be made to exported APIs before v1.0.0.

## 💡 Usage

GoDoc: [https://pkg.go.dev/github.com/samber/slog-datadog](https://pkg.go.dev/github.com/samber/slog-datadog)

### Handler options

```go

type Option struct {
	// log level (default: debug)
	Level slog.Leveler

	// datadog endpoint
	Client  *datadog.APIClient
	Context context.Context

	// source parameters
	Service    string
	Hostname   string
	GlobalTags map[string]string

	// optional: customize Datadog message builder
	Converter Converter
}
```

Attributes will be injected in log payload.

### Example

```go
import (
	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	slogdatadog "github.com/samber/slog-datadog"
	"golang.org/x/exp/slog"
)

func newDatadogClient(endpoint string, apiKey string) (*datadog.APIClient, context.Context) {
	ctx := datadog.NewDefaultContext(context.Background())
	ctx = context.WithValue(
		ctx,
		datadog.ContextAPIKeys,
		map[string]datadog.APIKey{"apiKeyAuth": {Key: apiKey}},
	)
	ctx = context.WithValue(
		ctx,
		datadog.ContextServerVariables,
		map[string]string{"site": endpoint},
	)
	configuration := datadog.NewConfiguration()
	apiClient := datadog.NewAPIClient(configuration)

	return apiClient, ctx
}

func main() {
    host := "1.2.3.4"
	service := "api"
	endpoint := slogdatadog.DatadogHostEU
	apiKey := "xxx"
	apiClient, ctx := newDatadogClient(endpoint, apiKey)

	logger := slog.New(slogdatadog.Option{Level: slog.LevelDebug, Client: apiClient, Context: ctx, Hostname: host, Service: service}.NewDatadogHandler())
    logger = logger.
        With("environment", "dev").
        With("release", "v1.0.0")

    // log error
    logger.
        With("category", "sql").
        With("query.statement", "SELECT COUNT(*) FROM users;").
        With("query.duration", 1*time.Second).
        With("error", fmt.Errorf("could not count users")).
        Error("caramba!")

    // log user signup
    logger.
        With(
            slog.Group("user",
                slog.String("id", "user-123"),
                slog.Time("created_at", time.Now()),
            ),
        ).
        Info("user registration")
}
```

## 🤝 Contributing

- Ping me on twitter [@samuelberthe](https://twitter.com/samuelberthe) (DMs, mentions, whatever :))
- Fork the [project](https://github.com/samber/slog-datadog)
- Fix [open issues](https://github.com/samber/slog-datadog/issues) or request new features

Don't hesitate ;)

```bash
# Install some dev dependencies
make tools

# Run tests
make test
# or
make watch-test
```

## 👤 Contributors

![Contributors](https://contrib.rocks/image?repo=samber/slog-datadog)

## 💫 Show your support

Give a ⭐️ if this project helped you!

[![GitHub Sponsors](https://img.shields.io/github/sponsors/samber?style=for-the-badge)](https://github.com/sponsors/samber)

## 📝 License

Copyright © 2023 [Samuel Berthe](https://github.com/samber).

This project is [MIT](./LICENSE) licensed.
