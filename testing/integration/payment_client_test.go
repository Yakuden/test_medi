package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	wiremock "github.com/wiremock/go-wiremock"

	"github.com/yakudenn/meddetch-task/client"
	"github.com/yakudenn/meddetch-task/testing/setup"
)

func TestPaymentClient_Success(t *testing.T) {
	wm := setup.StartWireMock(t)
	wmc := wiremock.NewClient(wm.BaseURL)

	err := wmc.StubFor(
		wiremock.Post(wiremock.URLEqualTo("/charge")).
			WillReturnResponse(
				wiremock.NewResponse().
					WithStatus(http.StatusOK).
					WithBody(`{"transaction_id":"txn-abc","status":"success"}`).
					WithHeaders(map[string]string{"Content-Type": "application/json"}),
			),
	)
	require.NoError(t, err)

	c := client.NewPaymentClient(wm.BaseURL, 5*time.Second)
	resp, err := c.Charge(context.Background(), &client.PaymentRequest{
		OrderID: "order-1", Amount: 99.99, Currency: "USD",
	})

	require.NoError(t, err)
	assert.Equal(t, "txn-abc", resp.TransactionID)
	assert.Equal(t, "success", resp.Status)
}

func TestPaymentClient_InsufficientFunds(t *testing.T) {
	wm := setup.StartWireMock(t)
	wmc := wiremock.NewClient(wm.BaseURL)

	err := wmc.StubFor(
		wiremock.Post(wiremock.URLEqualTo("/charge")).
			WillReturnResponse(
				wiremock.NewResponse().
					WithStatus(http.StatusPaymentRequired).
					WithBody(`{"status":"failure","message":"insufficient funds"}`).
					WithHeaders(map[string]string{"Content-Type": "application/json"}),
			),
	)
	require.NoError(t, err)

	c := client.NewPaymentClient(wm.BaseURL, 5*time.Second)
	_, err = c.Charge(context.Background(), &client.PaymentRequest{
		OrderID: "order-2", Amount: 99999.99, Currency: "USD",
	})

	require.Error(t, err)
	var payErr *client.PaymentError
	require.ErrorAs(t, err, &payErr)
	assert.Equal(t, http.StatusPaymentRequired, payErr.Code)
}

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

	require.Error(t, err)
}
