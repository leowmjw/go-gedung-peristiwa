package httpapi

import (
	"app/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client is an HTTP client for sending metrics
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new HTTP metrics client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// SendMetrics sends a batch of metrics to the server
func (c *Client) SendMetrics(ctx context.Context, metrics []shared.MetricPoint) error {
	reqBody := HTTPMetricRequest{
		Metrics: metrics,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, 
		c.baseURL+"/metrics/v1/batch", 
		bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var response HTTPMetricResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("server error: %s", response.Error)
	}

	return nil
}
