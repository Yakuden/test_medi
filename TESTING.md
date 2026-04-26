# Integration Testing Strategy

## The over-mocking problem

```go
// This test passes but misses real bugs:
mockRepo.On("Create", mock.Anything).Return(&Order{ID: "1"}, nil)
```

The mock says "Create returns an order with ID 1". It never runs SQL. It never checks that the `NUMERIC` column round-trips a float64 without precision loss. It never verifies that `gen_random_uuid()` actually generates a UUID. The test is testing that your code calls the mock — not that it does anything useful.

## Test pyramid

| Test Level | What to Test | What to Mock |
|------------|--------------|--------------|
| Unit | Business rules: status transitions, validation, domain event emission, error paths in the usecase (given repo/payment return specific values) | Repository, PaymentClient, Clock |
| Integration | SQL correctness, data serialization, DB constraints, HTTP client JSON parsing, timeout/retry behavior | Nothing — use real Postgres (testcontainers) and real WireMock |
| E2E | Full flow through HTTP → usecase → DB → payment stub → response | External payment gateway only (WireMock or staging) |

**Rule:** mock at the boundary, not in the middle. The repository interface is the boundary between usecase and DB — unit tests mock it. The integration test exercises the real implementation of that boundary.

## Repository integration test

Tests real SQL against a real Postgres instance in a container. Verifies:
- `Create` persists the order and returns it with a generated ID
- `UpdateStatus` changes only the status field, not amount or customer
- `GetByID` returns `domain.ErrOrderNotFound` for missing records (not a generic error)
- `ListPending` filters by status correctly

```go
// testing/integration/order_repo_test.go

func TestOrderRepo_Create(t *testing.T) {
    pg := setup.StartPostgres(t)
    repo := repository.New(pg.Pool, domain.RealClock{})

    o, err := domain.NewOrder("cust-1", "prod-A", 49.99)
    require.NoError(t, err)

    got, err := repo.Create(ctx, o)
    require.NoError(t, err)
    assert.NotEmpty(t, got.ID)
    assert.Equal(t, domain.StatusPending, got.Status)

    fromDB, err := repo.GetByID(ctx, got.ID)
    require.NoError(t, err)
    assert.InDelta(t, got.Amount, fromDB.Amount, 0.001) // NUMERIC → float64 round-trip
}
```

`assert.InDelta` instead of `assert.Equal` for amount: `NUMERIC` in Postgres and `float64` in Go may differ by rounding in the last few ULP.

`testcontainers.StartPostgres` starts a fresh container per test, applies the schema, and registers cleanup via `t.Cleanup`. Each test gets an isolated DB state — no shared state between tests.

## External service test (WireMock)

Tests the HTTP client against a real HTTP server (WireMock in a container). Verifies:
- JSON serialization of the request body
- Correct parsing of the success response
- `PaymentError` is returned with the right status code on 4xx
- Client-side timeout fires when the server delays beyond the configured threshold

```go
// testing/integration/payment_client_test.go

func TestPaymentClient_Timeout(t *testing.T) {
    wm := setup.StartWireMock(t)
    wmc := wiremock.NewClient(wm.BaseURL)

    err := wmc.StubFor(
        wiremock.Post(wiremock.URLEqualTo("/charge")).
            WillReturnResponse(
                wiremock.NewResponse().
                    WithStatus(http.StatusOK).
                    WithBody(`{"status":"ok"}`).
                    WithFixedDelay(3000 * time.Millisecond),
            ),
    )
    require.NoError(t, err)

    c := client.NewPaymentClient(wm.BaseURL, 500*time.Millisecond)
    _, err = c.Charge(context.Background(), &client.PaymentRequest{
        OrderID: "order-3", Amount: 1.00, Currency: "USD",
    })

    require.Error(t, err) // deadline exceeded — client timed out before server responded
}
```

The 500ms client timeout fires well before WireMock's 3s delay. This test would silently pass if the payment client did not set a timeout on the underlying `http.Client` — that bug is invisible to a mock.

## Time-dependent tests

The domain uses a `Clock` interface (`domain.RealClock` / `domain.FixedClock`). Integration tests that need to assert on `CreatedAt` or `UpdatedAt` values pass `domain.FixedClock{T: knownTime}` to the repository. The value in the DB is deterministic; the assertion is exact.

```go
repo := repository.New(pg.Pool, domain.FixedClock{T: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
```

Never call `time.Now()` on both sides of an assertion — clock skew between the two calls makes the test intermittently fail in CI.
