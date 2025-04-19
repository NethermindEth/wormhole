package aztec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// HTTPClient provides a wrapper for HTTP requests with retries and timeouts
type HTTPClient interface {
	DoRequest(ctx context.Context, url string, payload map[string]any) ([]byte, error)
}

// httpClient is the default implementation of HTTPClient
type httpClient struct {
	client            *http.Client
	maxRetries        int
	initialBackoff    time.Duration
	backoffMultiplier float64
	logger            *zap.Logger
}

// NewHTTPClient creates a new HTTP client with specified configuration
func NewHTTPClient(timeout time.Duration, maxRetries int, initialBackoff time.Duration, backoffMultiplier float64, logger *zap.Logger) HTTPClient {
	return &httpClient{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		maxRetries:        maxRetries,
		initialBackoff:    initialBackoff,
		backoffMultiplier: backoffMultiplier,
		logger:            logger,
	}
}

// DoRequest sends an HTTP request with retries
func (c *httpClient) DoRequest(ctx context.Context, url string, payload map[string]any) ([]byte, error) {
	// Marshal the payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON: %w", err)
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Log the request details at debug level
	c.logger.Debug("Sending request",
		zap.String("url", url),
		zap.String("method", req.Method),
		zap.Any("payload", payload))

	// Execute with retries
	return c.doRequestWithRetry(ctx, req)
}

// doRequestWithRetry implements exponential backoff retry logic
func (c *httpClient) doRequestWithRetry(ctx context.Context, req *http.Request) ([]byte, error) {
	var lastErr error
	backoff := c.initialBackoff

	method := "unknown"
	if len(req.URL.Path) > 0 {
		method = req.URL.Path[1:] // Remove leading slash
	}

	for retry := 0; retry <= c.maxRetries; retry++ {
		// Check if context is canceled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// Continue with the request
		}

		if retry > 0 {
			c.logger.Debug("Retrying request",
				zap.String("url", req.URL.String()),
				zap.Int("attempt", retry+1),
				zap.Duration("backoff", backoff))
		}

		// Execute the request
		start := time.Now()
		resp, err := c.client.Do(req)
		duration := time.Since(start)

		// Check for request errors
		if err != nil {
			c.logger.Warn("Request failed",
				zap.String("url", req.URL.String()),
				zap.Duration("duration", duration),
				zap.Error(err))
			lastErr = err

			// Wait before retrying
			if retry < c.maxRetries {
				select {
				case <-time.After(backoff):
					backoff = time.Duration(float64(backoff) * c.backoffMultiplier)
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, fmt.Errorf("request error after %d attempts: %w", retry+1, err)
		}
		defer resp.Body.Close()

		// Check status code
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			c.logger.Warn("Non-200 status code",
				zap.String("url", req.URL.String()),
				zap.Int("status", resp.StatusCode),
				zap.String("response", string(body)),
				zap.Duration("duration", duration))

			lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)

			// Retry on server errors (5xx)
			if resp.StatusCode >= 500 && retry < c.maxRetries {
				select {
				case <-time.After(backoff):
					backoff = time.Duration(float64(backoff) * c.backoffMultiplier)
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}

			return nil, lastErr
		}

		// Read the response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger.Warn("Error reading response",
				zap.String("url", req.URL.String()),
				zap.Duration("duration", duration),
				zap.Error(err))
			lastErr = fmt.Errorf("error reading response: %w", err)

			if retry < c.maxRetries {
				select {
				case <-time.After(backoff):
					backoff = time.Duration(float64(backoff) * c.backoffMultiplier)
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, lastErr
		}

		// Check for JSON-RPC errors in the response
		hasError, rpcError := GetJSONRPCError(body)
		if hasError {
			c.logger.Warn("JSON-RPC error",
				zap.String("url", req.URL.String()),
				zap.Int("code", rpcError.Code),
				zap.String("message", rpcError.Msg),
				zap.Duration("duration", duration))

			lastErr = rpcError

			// Retry on server errors
			if IsRetryableRPCError(rpcError) && retry < c.maxRetries {
				select {
				case <-time.After(backoff):
					backoff = time.Duration(float64(backoff) * c.backoffMultiplier)
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}

			return nil, lastErr
		}

		// Log successful request
		c.logger.Debug("Request successful",
			zap.String("url", req.URL.String()),
			zap.Duration("duration", duration),
			zap.Int("response_size", len(body)))

		return body, nil
	}

	return nil, &ErrMaxRetriesExceeded{Method: method}
}
