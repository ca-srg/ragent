package observability

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const defaultShutdownTimeout = 5 * time.Second

// ShutdownFunc represents a graceful shutdown handler that waits for exporters to flush.
type ShutdownFunc func(context.Context) error

// NewShutdownFunc coordinates tracer and meter shutdown logic.
func NewShutdownFunc(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider) ShutdownFunc {
	return func(ctx context.Context) error {
		shutdownCtx, cancel := ensureShutdownContext(ctx)
		defer cancel()

		var errs []error

		if tp != nil {
			if err := tp.Shutdown(shutdownCtx); err != nil {
				log.Printf("observability: failed to shutdown tracer provider: %v", err)
				errs = append(errs, fmt.Errorf("tracer provider: %w", err))
			}
		}

		if mp != nil {
			if err := mp.Shutdown(shutdownCtx); err != nil {
				log.Printf("observability: failed to shutdown meter provider: %v", err)
				errs = append(errs, fmt.Errorf("meter provider: %w", err))
			}
		}

		if len(errs) == 0 {
			return nil
		}

		return errors.Join(errs...)
	}
}

func ensureShutdownContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), defaultShutdownTimeout)
	}

	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, defaultShutdownTimeout)
}
