# Tracing Design

## Where trace context lives

### Usecases — Option A (context values)

`context.Context` is the idiomatic Go carrier for request-scoped data. OTel's `trace.SpanFromContext` is built for this: the span travels with the request through the call stack without polluting every function signature.

Option B (explicit `TraceContext` parameter) duplicates what `context` already does and requires changing every caller. Option C (decorator) is useful for coarse-grained boundary spans but adds a layer of indirection when the usecase needs fine-grained child spans at specific steps.

```go
// observability/tracing.go
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
    return otel.Tracer(tracerName).Start(ctx, name, opts...)
}

// usecase/create_order.go
func (uc *CreateOrderInteractor) Execute(ctx context.Context, req *CreateOrderRequest) (*domain.Order, error) {
    ctx, sp := observability.StartSpan(ctx, "CreateOrder.Execute")
    defer sp.End()

    // child span for the external call
    ctx, paySpan := observability.StartSpan(ctx, "PaymentClient.Charge")
    _, err = uc.payment.Charge(ctx, ...)
    paySpan.End()
}
```

### Domain methods — no context

The domain layer must stay pure: no infrastructure imports, no OTel. `Order.Complete()` contains only business rules. If it needed `context.Context`, it would have to import `go.opentelemetry.io/otel/trace` — infrastructure in the domain.

The usecase creates a span *around* the domain call, not inside it.

### Repository methods — Option A (context values)

Repositories need `ctx` already: pgx passes it to every query for timeout and cancellation. Since the active span rides in `ctx`, OTel's pgx instrumentation picks it up transparently. No additional parameters required.

## Tracing domain operations without injecting context

Two approaches that keep the domain pure:

### 1. Span-around + domain events bridge (used here)

The usecase wraps the domain call in a child span. Domain methods emit `DomainEvent` values that carry business data. The usecase bridges them to span events after the call.

```go
if err := order.Complete(); err != nil {
    observability.RecordError(ctx, err)
    return nil, err
}

events := order.PopEvents() // drain before UpdateStatus
order, err = uc.orders.UpdateStatus(ctx, order.ID, order.Status)

for _, ev := range events {
    observability.SpanEvent(ctx, ev.Name, ev.Data)
}
```

`PopEvents` drains the slice; the usecase decides what to do with each event — span event, publish to queue, audit log.

### 2. Decorator on the usecase interface

Wrap the usecase, not the domain. The decorator adds the outer span; the inner usecase adds child spans for individual steps.

```go
type TracedCreateOrder struct {
    inner  *CreateOrderInteractor
    tracer trace.Tracer
}

func (t *TracedCreateOrder) Execute(ctx context.Context, req *CreateOrderRequest) (*domain.Order, error) {
    ctx, sp := t.tracer.Start(ctx, "CreateOrder")
    defer sp.End()
    return t.inner.Execute(ctx, req)
}
```

Useful when you want a consistent entry span without touching the usecase implementation — e.g., applying tracing to every usecase through a common interface.

## HTTP propagation

Incoming requests: `observability.ExtractHTTP(r)` reads `traceparent`/`tracestate` headers and returns a context with the remote span as parent.

Outgoing calls: `observability.InjectHTTP(ctx, r)` writes those headers onto the outbound request so downstream services (Inventory, Payment) join the same trace.

```go
// observability/tracing.go
func InjectHTTP(ctx context.Context, r *http.Request) {
    otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(r.Header))
}

func ExtractHTTP(r *http.Request) context.Context {
    return otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
}
```

The HTTP middleware (`observability.Middleware`) wraps every handler: it extracts the incoming trace context, starts a server span, and propagates the enriched context to the handler.
