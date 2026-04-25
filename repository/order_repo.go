package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yakudenn/meddetch-task/domain"
)

type OrderRepo struct {
	db    *pgxpool.Pool
	clock domain.Clock
}

func New(db *pgxpool.Pool, clock domain.Clock) *OrderRepo {
	return &OrderRepo{db: db, clock: clock}
}

func (r *OrderRepo) Create(ctx context.Context, o *domain.Order) (*domain.Order, error) {
	now := r.clock.Now().UTC().Truncate(time.Microsecond)
	o.CreatedAt = now
	o.UpdatedAt = now

	const q = `
		INSERT INTO orders (customer_id, product_id, amount, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id`

	err := r.db.QueryRow(ctx, q,
		o.CustomerID, o.ProductID, o.Amount, string(o.Status), now, now,
	).Scan(&o.ID)
	if err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}
	return o, nil
}

func (r *OrderRepo) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	const q = `
		SELECT id, customer_id, product_id, amount, status, created_at, updated_at
		FROM orders WHERE id = $1`

	o := &domain.Order{}
	err := r.db.QueryRow(ctx, q, id).Scan(
		&o.ID, &o.CustomerID, &o.ProductID, &o.Amount, &o.Status, &o.CreatedAt, &o.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	return o, nil
}

func (r *OrderRepo) UpdateStatus(ctx context.Context, id string, status domain.OrderStatus) (*domain.Order, error) {
	const q = `
		UPDATE orders SET status=$1, updated_at=$2
		WHERE id=$3
		RETURNING id, customer_id, product_id, amount, status, created_at, updated_at`

	now := r.clock.Now().UTC()
	o := &domain.Order{}
	err := r.db.QueryRow(ctx, q, string(status), now, id).Scan(
		&o.ID, &o.CustomerID, &o.ProductID, &o.Amount, &o.Status, &o.CreatedAt, &o.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}
	return o, nil
}

func (r *OrderRepo) ListPending(ctx context.Context) ([]*domain.Order, error) {
	const q = `
		SELECT id, customer_id, product_id, amount, status, created_at, updated_at
		FROM orders WHERE status = 'pending' ORDER BY created_at`

	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var orders []*domain.Order
	for rows.Next() {
		o := &domain.Order{}
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.ProductID, &o.Amount, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

var _ domain.OrderRepository = (*OrderRepo)(nil)
