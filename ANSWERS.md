# Answers

## Q1: Tracing domain operations without passing context

**Approach 1: Span-around + domain events bridge**

The usecase creates a child span around the domain call. The domain method emits `DomainEvent` values into a slice on the aggregate. After the call, the usecase drains those events and records them as span events.

```go
if err := order.Complete(); err != nil {
    observability.RecordError(ctx, err)
    return nil, err
}
events := order.PopEvents()

order, err = uc.orders.UpdateStatus(ctx, order.ID, order.Status)
if err != nil { ... }

for _, ev := range events {
    observability.SpanEvent(ctx, ev.Name, ev.Data)
}
```

The domain stays pure: no `context`, no OTel imports. The usecase decides what to do with domain events — span event, message queue publish, audit log.

**Approach 2: Decorator on the usecase interface**

Wrap the usecase in a tracing decorator. The domain is never aware of tracing at all; the decorator adds the entry span before delegating to the real usecase.

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

Useful when you want a consistent outer span for every usecase through a common interface, without touching each implementation.

---

## Q2: Mocking — right choice, wrong choice, need both

**When mocking is the right choice**

Unit-testing business logic where the dependency is slow, flaky, or irrelevant to the rule being tested. Example: testing that `CreateOrderInteractor.Execute` returns `ErrInvalidAmount` when `amount <= 0` — you don't need a DB or a payment gateway to verify that. The mock lets the test run in microseconds and isolates the exact behavior being checked.

**When mocking hides bugs**

Mocking the repository in integration-style tests. Example:

```go
mockRepo.On("Create", mock.Anything).Return(&Order{ID: "generated-id"}, nil)
```

This hides:
- The SQL `INSERT` has a typo in a column name — would fail on a real DB but the mock never runs SQL.
- `amount NUMERIC` precision: `49.99` stored and retrieved as `49.9899999999...` — the mock returns exactly what you put in, masking the round-trip loss.
- `gen_random_uuid()` is missing from the DB schema — mock always returns a hardcoded ID, the real column default would error.

**When you need both**

HTTP client behavior. A mock test verifies the usecase handles a payment timeout correctly (returns an error, marks the order failed, emits the right events) — the mock responds instantly with a timeout error, no container needed. An integration test (WireMock) verifies the HTTP client actually respects the configured `http.Client.Timeout` and that the JSON serialization of `PaymentRequest` matches what the real API expects. The mock test can't catch a missing `Content-Type` header; the integration test can't easily simulate all error paths cheaply.

---

## Q3: "Order charged but shows as failed" — debugging workflow

**Logs**

Search by `order_id` across all services. The critical sequence to find:

1. `"payment failed"` log with the order_id — was there a payment error, or did the usecase think it succeeded?
2. If payment succeeded: look for `"save order"` error after the payment step — `UpdateStatus` may have failed, leaving the order in `pending` or `failed` state while the charge went through.
3. Check the `transaction_id` from the payment response — query the payment provider's logs to confirm whether the charge was actually captured.

Structured logs with `trace_id` make this feasible: pull the full trace from Jaeger, find every log line with the same `trace_id`, reconstruct the exact sequence.

**Metrics**

- `orders_created_total{status="failure"}` spike without a corresponding spike in `orders_created_total{status="success"}` — orders are failing at the final step, not at payment.
- `order_processing_duration_seconds{step="fulfillment"}` high p99 — the DB write after payment is slow or timing out.
- `orders_pending_count` not decreasing — orders are getting stuck, not transitioning to completed or failed.

**Alerts**

- Alert on `rate(orders_created_total{status="failure"}[5m]) / rate(orders_created_total[5m]) > 0.05` for 2 minutes — more than 5% failure rate.
- Alert on `orders_pending_count > threshold` for 10 minutes — stuck orders.
- Cross-system alert: if the payment provider's success rate is normal but our `orders_created_total{status="failure"}` is elevated, the bug is in our post-payment persistence, not in the payment step.

---

## Q4: Testing the outbox pattern without flaky timing

The root cause of flakiness is asserting on the result of an asynchronous process with a fixed `time.Sleep`. The sleep is either too short (test fails in slow CI) or too long (test suite is slow).

**Approach 1: Make the worker synchronous in tests (preferred)**

Extract the processing logic into a `func ProcessOutbox(ctx context.Context, db DB, queue Queue) error` that reads one batch from the outbox, publishes to the queue, and marks entries processed. In tests, call it directly — no goroutines, no timing, deterministic.

```go
// Test
written, _ := repo.CreateWithOutbox(ctx, order)
err := worker.ProcessOutbox(ctx, db, queue)
require.NoError(t, err)

msg := queue.Dequeue()
assert.Equal(t, written.ID, msg.OrderID)

entry, _ := db.GetOutboxEntry(ctx, written.OutboxID)
assert.True(t, entry.Processed)
```

The worker code is the same in production (called on a ticker) and in tests (called directly).

**Approach 2: `assert.Eventually` with a short poll interval**

When the worker must run asynchronously (e.g., testing the ticker behavior itself):

```go
assert.Eventually(t, func() bool {
    entry, err := db.GetOutboxEntry(ctx, id)
    return err == nil && entry.Processed
}, 5*time.Second, 50*time.Millisecond)
```

`assert.Eventually` polls every 50ms up to a 5s deadline. This tolerates scheduler jitter without a fixed sleep. Combined with testcontainers (real DB, real queue), this tests the actual end-to-end flow.

---

## Q5: Flaky time-dependent tests

**What's wrong**

```go
order.CreatedAt = time.Now()  // captured at T1
// ... code runs ...
assert.Equal(t, time.Now(), order.CreatedAt)  // compared at T2
```

`time.Now()` at T1 and T2 are different values. `assert.Equal` does exact comparison — even a nanosecond difference fails the test. In CI, the OS scheduler may pause the goroutine between the two calls, making the difference larger and the failure more frequent.

**How to fix**

Inject a `Clock` interface and use `FixedClock` in tests. The domain already has this:

```go
type Clock interface { Now() time.Time }
type FixedClock struct{ T time.Time }
func (c FixedClock) Now() time.Time { return c.T }
```

In the repository and domain, never call `time.Now()` directly — call `clock.Now()`. In tests:

```go
fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
repo := repository.New(pool, domain.FixedClock{T: fixed})

order, _ := repo.Create(ctx, o)
assert.Equal(t, fixed, order.CreatedAt) // always passes
```

The test controls what "now" means. The assertion is exact and deterministic regardless of CI load.
