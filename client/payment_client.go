package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type PaymentRequest struct {
	OrderID  string  `json:"order_id"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type PaymentResponse struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
}

type PaymentError struct {
	Code    int
	Message string
}

func (e *PaymentError) Error() string {
	return fmt.Sprintf("payment api %d: %s", e.Code, e.Message)
}

type PaymentClient struct {
	baseURL string
	http    *http.Client
}

func NewPaymentClient(baseURL string, timeout time.Duration) *PaymentClient {
	return &PaymentClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *PaymentClient) Charge(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/charge", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("charge: %w", err)
	}
	defer resp.Body.Close()

	var out PaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &PaymentError{Code: resp.StatusCode, Message: out.Message}
	}
	return &out, nil
}
