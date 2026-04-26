package setup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const schema = `
CREATE TABLE IF NOT EXISTS orders (
	id          TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::text,
	customer_id TEXT        NOT NULL,
	product_id  TEXT        NOT NULL,
	amount      NUMERIC     NOT NULL,
	status      TEXT        NOT NULL DEFAULT 'pending',
	created_at  TIMESTAMPTZ NOT NULL,
	updated_at  TIMESTAMPTZ NOT NULL
);`

type PG struct {
	Pool *pgxpool.Pool
	DSN  string
}

func StartPostgres(t *testing.T) *PG {
	t.Helper()
	ctx := context.Background()

	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return &PG{Pool: pool, DSN: dsn}
}

type WireMock struct {
	BaseURL string
}

func StartWireMock(t *testing.T) *WireMock {
	t.Helper()
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "wiremock/wiremock:3.6.0",
			ExposedPorts: []string{"8080/tcp"},
			WaitingFor: wait.ForHTTP("/__admin/mappings").
				WithPort("8080/tcp").
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start wiremock: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	host, _ := ctr.Host(ctx)
	port, _ := ctr.MappedPort(ctx, "8080/tcp")
	return &WireMock{BaseURL: fmt.Sprintf("http://%s:%s", host, port.Port())}
}
