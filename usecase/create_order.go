package usecase

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/yakudenn/meddetch-task/client"
	"github.com/yakudenn/meddetch-task/domain"
	"github.com/yakudenn/meddetch-task/observability"
)

type CreateOrderRequest struct {
	CustomerID   string
	CustomerTier string
	ProductID    string
	Amount       float64
}

type CreateOrderInteractor struct {
	orders  domain.OrderRepository
	payment *client.PaymentClient
	metrics *observability.OrderMetrics
	log     *observability.Logger
}

func NewCreateOrder(
	orders domain.OrderRepository,
	payment *client.PaymentClient,
	metrics *observability.OrderMetrics,
	log *observability.Logger,
) *CreateOrderInteractor {
	return &CreateOrderInteractor{
		orders:  orders,
		payment: payment,
		metrics: metrics,
		log:     log,
	}
}

func (uc *CreateOrderInteractor) Execute(ctx context.Context, req *CreateOrderRequest) (*domain.Order, error) {
	ctx, sp := observability.StartSpan(ctx, "CreateOrder.Execute")
	defer sp.End()

	order, err := domain.NewOrder(req.CustomerID, req.ProductID, req.Amount)
	if err != nil {
		observability.RecordError(ctx, err)
		return nil, err
	}

	t := time.Now()
	order, err = uc.orders.Create(ctx, order)
	uc.metrics.ObserveStep("validation", time.Since(t))
	if err != nil {
		observability.RecordError(ctx, err)
		uc.metrics.IncOrders("failure", req.CustomerTier)
		return nil, fmt.Errorf("persist order: %w", err)
	}

	t = time.Now()
	ctx, paySpan := observability.StartSpan(ctx, "PaymentClient.Charge")
	_, err = uc.payment.Charge(ctx, &client.PaymentRequest{
		OrderID:  order.ID,
		Amount:   order.Amount,
		Currency: "USD",
	})
	paySpan.End()
	uc.metrics.ObserveStep("payment", time.Since(t))

	if err != nil {
		_ = order.Fail("payment error")
		_, _ = uc.orders.UpdateStatus(ctx, order.ID, order.Status)
		for _, ev := range order.PopEvents() {
			observability.SpanEvent(ctx, ev.Name, ev.Data)
		}
		observability.RecordError(ctx, err)
		uc.metrics.IncOrders("failure", req.CustomerTier)
		uc.log.Error(ctx, "payment failed", zap.String("order_id", order.ID), zap.Error(err))
		return nil, fmt.Errorf("charge: %w", err)
	}

	t = time.Now()
	if err := order.Complete(); err != nil {
		observability.RecordError(ctx, err)
		return nil, err
	}

	events := order.PopEvents()

	order, err = uc.orders.UpdateStatus(ctx, order.ID, order.Status)
	uc.metrics.ObserveStep("fulfillment", time.Since(t))
	if err != nil {
		observability.RecordError(ctx, err)
		return nil, fmt.Errorf("save order: %w", err)
	}

	for _, ev := range events {
		observability.SpanEvent(ctx, ev.Name, ev.Data)
	}

	uc.metrics.IncOrders("success", req.CustomerTier)
	uc.log.Info(ctx, "order created", zap.String("order_id", order.ID))
	return order, nil
}
