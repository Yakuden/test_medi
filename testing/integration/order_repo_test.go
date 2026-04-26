package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yakudenn/meddetch-task/domain"
	"github.com/yakudenn/meddetch-task/repository"
	"github.com/yakudenn/meddetch-task/testing/setup"
)

func TestOrderRepo_Create(t *testing.T) {
	pg := setup.StartPostgres(t)
	repo := repository.New(pg.Pool, domain.RealClock{})
	ctx := context.Background()

	o, err := domain.NewOrder("cust-1", "prod-A", 49.99)
	require.NoError(t, err)

	got, err := repo.Create(ctx, o)
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, domain.StatusPending, got.Status)
	assert.False(t, got.CreatedAt.IsZero())

	fromDB, err := repo.GetByID(ctx, got.ID)
	require.NoError(t, err)
	assert.Equal(t, got.CustomerID, fromDB.CustomerID)
	assert.InDelta(t, got.Amount, fromDB.Amount, 0.001)
}

func TestOrderRepo_UpdateStatus_OnlyStatusChanges(t *testing.T) {
	pg := setup.StartPostgres(t)
	repo := repository.New(pg.Pool, domain.RealClock{})
	ctx := context.Background()

	o, _ := domain.NewOrder("cust-2", "prod-B", 100.00)
	created, err := repo.Create(ctx, o)
	require.NoError(t, err)

	updated, err := repo.UpdateStatus(ctx, created.ID, domain.StatusCompleted)
	require.NoError(t, err)

	assert.Equal(t, domain.StatusCompleted, updated.Status)
	assert.Equal(t, created.CustomerID, updated.CustomerID)
	assert.InDelta(t, created.Amount, updated.Amount, 0.001)
	assert.Equal(t, created.CreatedAt.UTC(), updated.CreatedAt.UTC())
}

func TestOrderRepo_GetByID_NotFound(t *testing.T) {
	pg := setup.StartPostgres(t)
	repo := repository.New(pg.Pool, domain.RealClock{})

	_, err := repo.GetByID(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, domain.ErrOrderNotFound)
}

func TestOrderRepo_ListPending(t *testing.T) {
	pg := setup.StartPostgres(t)
	repo := repository.New(pg.Pool, domain.RealClock{})
	ctx := context.Background()

	for _, cust := range []string{"a", "b"} {
		o, _ := domain.NewOrder(cust, "prod-X", 10.00)
		_, err := repo.Create(ctx, o)
		require.NoError(t, err)
	}

	o, _ := domain.NewOrder("c", "prod-X", 10.00)
	completed, _ := repo.Create(ctx, o)
	_, err := repo.UpdateStatus(ctx, completed.ID, domain.StatusCompleted)
	require.NoError(t, err)

	pending, err := repo.ListPending(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, 2)
}
