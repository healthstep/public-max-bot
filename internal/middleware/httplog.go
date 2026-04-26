package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/porebric/logger"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return h.Hijack()
}

// AccessLog logs one line per HTTP request; trace_id on context.
func AccessLog() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := obs.WithTrace(r.Context())
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 0}
			next.ServeHTTP(sw, r.WithContext(ctx))
			code := sw.status
			if code == 0 {
				code = http.StatusOK
			}
			ms := time.Since(start).Milliseconds()
			if code >= 500 {
				logger.Error(ctx, fmt.Errorf("http status %d", code), "http access",
					"method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery,
					"status", code, "duration_ms", ms, "remote", r.RemoteAddr, "user_agent", r.UserAgent())
			} else if code >= 400 {
				logger.Warn(ctx, "http access",
					"method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery,
					"status", code, "duration_ms", ms, "remote", r.RemoteAddr, "user_agent", r.UserAgent())
			} else {
				logger.Info(ctx, "http access",
					"method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery,
					"status", code, "duration_ms", ms, "remote", r.RemoteAddr, "user_agent", r.UserAgent())
			}
		})
	}
}
