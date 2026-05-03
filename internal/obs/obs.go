package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/porebric/logger"
	otrace "go.opentelemetry.io/otel/trace"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var L *logger.Logger

func init() {
	_ = prometheus.DefaultRegisterer.Register(collectors.NewGoCollector())
	_ = prometheus.DefaultRegisterer.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

func Init(serviceName string) {
	L = logger.New(logger.InfoLevel).With("service", serviceName)
}

func TraceIDOrGen(ctx context.Context) string {
	if sc := otrace.SpanContextFromContext(ctx); sc.IsValid() && sc.HasTraceID() {
		return sc.TraceID().String()
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func WithTrace(ctx context.Context) context.Context {
	if L == nil {
		return ctx
	}
	return logger.ToContext(ctx, L.With("trace_id", TraceIDOrGen(ctx)))
}

func BG(component string) *logger.Logger {
	if L == nil {
		return logger.New(logger.InfoLevel)
	}
	return L.With("trace_id", "bg", "component", component)
}
