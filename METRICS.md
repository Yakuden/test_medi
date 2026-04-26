# Metrics Design

## Where to instrument

The usecase layer. It's the only place that knows:
- the business outcome (success vs failure)
- the customer tier (free vs premium)
- which pipeline step is being timed (validation, payment, fulfillment)

The repository doesn't know customer tier. The HTTP handler doesn't know which step failed. Infrastructure metrics (DB query duration, HTTP connection pool size) belong at the infrastructure layer — they complement usecase metrics, not replace them.

```go
// usecase/create_order.go

t := time.Now()
order, err = uc.orders.Create(ctx, order)
uc.metrics.ObserveStep("validation", time.Since(t))

t = time.Now()
_, err = uc.payment.Charge(ctx, ...)
uc.metrics.ObserveStep("payment", time.Since(t))

t = time.Now()
order, err = uc.orders.UpdateStatus(ctx, order.ID, order.Status)
uc.metrics.ObserveStep("fulfillment", time.Since(t))

uc.metrics.IncOrders("success", req.CustomerTier)
```

## Cardinality explosion

```go
// This creates one time series per (customerID, productID, orderID) combination.
// With 100k customers × 10k products × unbounded orders: millions of series.
orderDuration.WithLabelValues(customerID, productID, orderID).Observe(duration)
```

**Why it's a problem:** Prometheus stores one time series per unique label set in memory. High-cardinality labels (IDs, emails, UUIDs) create unbounded series, OOM-killing Prometheus or making it too slow to query.

**Fix:** only use low-cardinality labels — values with a small, known set of possible values.

```go
// observability/metrics.go — labels: status (2 values), customer_tier (2 values)
total: f.NewCounterVec(prometheus.CounterOpts{
    Name: "orders_created_total",
}, []string{"status", "customer_tier"}),

// label: step (3 values: validation, payment, fulfillment)
duration: f.NewHistogramVec(prometheus.HistogramOpts{
    Name: "order_processing_duration_seconds",
}, []string{"step"}),
```

If you need per-customer or per-order detail, that belongs in traces (Jaeger/Tempo), not metrics. Metrics answer "how is the system behaving in aggregate"; traces answer "what happened to this specific request".

## Correlating metrics with traces

Use **OTel exemplars**: histogram observations can carry a `trace_id` sample, linking a specific latency bucket to an actual trace in Jaeger.

```go
// When using prometheus/client_golang v1.16+ with native histograms:
observer.(prometheus.ExemplarObserver).ObserveWithExemplar(
    duration.Seconds(),
    prometheus.Labels{"trace_id": observability.TraceIDFromContext(ctx)},
)
```

Without exemplars, correlate by time: if `order_processing_duration_seconds{step="payment"}` shows a p99 spike at 14:32, query Jaeger for slow `PaymentClient.Charge` spans in that same 1-minute window.

For the pending gauge, update it after every status transition:

```go
// After UpdateStatus, re-read the count from the DB (or maintain in memory).
// The gauge reflects the current snapshot; Prometheus scrapes it on its schedule.
pending, _ := repo.ListPending(ctx)
metrics.SetPending(float64(len(pending)))
```
