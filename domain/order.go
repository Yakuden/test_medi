package domain

import (
	"context"
	"errors"
	"time"
)

type OrderStatus string

const (
	StatusPending   OrderStatus = "pending"
	StatusCompleted OrderStatus = "completed"
	StatusFailed    OrderStatus = "failed"
)

var (
	ErrOrderNotFound     = errors.New("order not found")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrInvalidAmount     = errors.New("amount must be positive")
)

type DomainEvent struct {
	Name string
	Data map[string]any
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type FixedClock struct{ T time.Time }

func (c FixedClock) Now() time.Time { return c.T }

type Order struct {
	ID         string
	CustomerID string
	ProductID  string
	Amount     float64
	Status     OrderStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time
	events     []DomainEvent
}

func NewOrder(customerID, productID string, amount float64) (*Order, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	return &Order{
		CustomerID: customerID,
		ProductID:  productID,
		Amount:     amount,
		Status:     StatusPending,
	}, nil
}

func (o *Order) Complete() error {
	if o.Status != StatusPending {
		return ErrInvalidTransition
	}
	o.Status = StatusCompleted
	o.emit(DomainEvent{
		Name: "order.completed",
		Data: map[string]any{"order_id": o.ID, "amount": o.Amount},
	})
	return nil
}

func (o *Order) Fail(reason string) error {
	if o.Status != StatusPending {
		return ErrInvalidTransition
	}
	o.Status = StatusFailed
	o.emit(DomainEvent{
		Name: "order.failed",
		Data: map[string]any{"order_id": o.ID, "reason": reason},
	})
	return nil
}

func (o *Order) emit(ev DomainEvent) {
	o.events = append(o.events, ev)
}

// PopEvents returns accumulated events and clears the slice.
func (o *Order) PopEvents() []DomainEvent {
	evs := make([]DomainEvent, len(o.events))
	copy(evs, o.events)
	o.events = nil
	return evs
}

type OrderRepository interface {
	Create(ctx context.Context, order *Order) (*Order, error)
	GetByID(ctx context.Context, id string) (*Order, error)
	UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error)
	ListPending(ctx context.Context) ([]*Order, error)
}
