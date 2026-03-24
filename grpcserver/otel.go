package grpcserver

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "arbiter"

func startEvalSpan(ctx context.Context, spanName string, bundleName string) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, spanName)
	span.SetAttributes(attribute.String("arbiter.bundle_name", bundleName))
	return ctx, span
}

func endEvalSpan(span trace.Span, matchCount int, err error) {
	span.SetAttributes(attribute.Int("arbiter.match_count", matchCount))
	if err != nil {
		span.RecordError(err)
	}
	span.End()
}
