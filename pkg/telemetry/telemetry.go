package telemetry

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/version"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const defaultEndpoint = "127.0.0.1:4317"

func Initialize(ctx context.Context) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	endpoint, insecure, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	exporterOptions := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if insecure {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, exporterOptions...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP gRPC trace exporter: %w", err)
	}

	resource, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			attribute.String("service.name", "kubernetes-event-exporter"),
			attribute.String("service.version", version.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTEL resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider.Shutdown, nil
}

func normalizeEndpoint(endpoint string) (string, bool, error) {
	if !strings.Contains(endpoint, "://") {
		return endpoint, true, nil
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("parse OTLP endpoint: %w", err)
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("OTLP endpoint has no host: %q", endpoint)
	}

	switch parsed.Scheme {
	case "http":
		return parsed.Host, true, nil
	case "https":
		return parsed.Host, false, nil
	default:
		return "", false, fmt.Errorf("unsupported OTLP endpoint scheme %q", parsed.Scheme)
	}
}
