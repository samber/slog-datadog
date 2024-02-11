package main

import (
	"context"
	"fmt"
	"time"

	"log/slog"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	slogdatadog "github.com/samber/slog-datadog/v2"
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
	apiKey := "xxxxx"
	apiClient, ctx := newDatadogClient(endpoint, apiKey)

	logger := slog.New(slogdatadog.Option{
		Level:     slog.LevelDebug,
		Client:    apiClient,
		Context:   ctx,
		Hostname:  host,
		Service:   service,
		AddSource: true,
	}.NewDatadogHandler())
	logger = logger.With("release", "v1.0.0")

	logger.
		With(
			slog.Group("user",
				slog.String("id", "user-123"),
				slog.Time("created_at", time.Now().AddDate(0, 0, -1)),
			),
		).
		With("environment", "dev").
		With("error", fmt.Errorf("an error")).
		Error("A message")
}
