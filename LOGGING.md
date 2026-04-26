# Logging Strategy

## Where logging happens

### Option A maintains Clean Architecture

The usecase logs *business-relevant* events: what decision was made and why. That's where business logic lives, so that's where business logs belong. Option B (service layer only) loses context — by the time you wrap the usecase call you only know it succeeded or failed, not which step failed or what the business state was. Option C (decorator) works for operational entry/exit logging but can't capture business-level detail without duplicating domain knowledge.

In practice the answer is A + C together: the usecase logs business events, a thin decorator at the HTTP handler logs request/response boundaries.

### Business logs vs operational logs

**Business logs** record decisions and state changes that matter to the domain: "order created", "payment failed — insufficient funds", "order completed". These live in the usecase. They answer *what happened* from the product's perspective.

**Operational logs** record infrastructure behavior: HTTP requests received, DB queries executed, retries attempted. These live at the boundary (HTTP middleware, repository, HTTP client). They answer *how the system behaved*.

Mixing them produces noise. A repository should not log "order 123 created" — it has no business context. A usecase should not log "SQL executed in 4ms" — that's instrumentation.

### Avoiding sensitive data

Three rules:
1. Never log raw request structs — they may contain card numbers, passwords, PII.
2. Redact at the point of logging, not after. Pass `observability.MaskedEmail(email)` as the field value.
3. Log identifiers (order_id, customer_id) not values (amount, card, address).

```go
// observability/logging.go
func Redact(s string) string { ... }           // keeps first 2 + last 2 chars
func MaskedEmail(email string) string { ... }  // user***@domain.com
func MaskedCard(card string) string { ... }    // ************1234
```

## Implementation

### Logger with automatic trace_id injection

Every log line gets `trace_id` and `span_id` from the active span in `ctx`. If there is no active span the fields are omitted silently — no sentinel values like `"unknown"`.

```go
// observability/logging.go
func (l *Logger) withCtx(ctx context.Context) *zap.Logger {
    tid := TraceIDFromContext(ctx)
    if tid == "" {
        return l.z
    }
    return l.z.With(
        zap.String("trace_id", tid),
        zap.String("span_id", SpanIDFromContext(ctx)),
    )
}

func (l *Logger) Info(ctx context.Context, msg string, fields ...zap.Field) {
    l.withCtx(ctx).Info(msg, fields...)
}
```

This means every log line in the usecase automatically correlates to the active trace in Jaeger/Tempo — no manual `zap.String("trace_id", ...)` at every call site.

### Request/response logging at boundaries

At the HTTP handler level (not in the usecase), log the incoming request shape and the outcome:

```go
func loggingMiddleware(log *observability.Logger, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        log.Info(ctx, "request",
            zap.String("method", r.Method),
            zap.String("path", r.URL.Path),
        )
        rw := &responseWriter{ResponseWriter: w}
        next.ServeHTTP(rw, r)
        log.Info(ctx, "response",
            zap.Int("status", rw.status),
        )
    })
}
```

The usecase does not know about HTTP — it logs business events only:

```go
// usecase/create_order.go
uc.log.Error(ctx, "payment failed",
    zap.String("order_id", order.ID),
    zap.Error(err),
)
uc.log.Info(ctx, "order created",
    zap.String("order_id", order.ID),
)
```

### Error logging with stack traces

zap's `zap.Error(err)` records the error message. For stack traces, wrap errors at origin with `fmt.Errorf("...: %w", err)` and use `zap.Error` — the full chain appears in the log. For unexpected errors (panics, infrastructure failures), use `zap.Stack("stack", zap.StackSkip(1))`.

The usecase always logs before returning an error so the context (order_id, customer_id) is captured at the point of failure, not just at the HTTP boundary where that context may be lost.
