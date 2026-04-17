package meraki

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// APIError is the base type for all Meraki API errors surfaced by the client.
type APIError struct {
	Status   int
	Endpoint string
	Message  string
	// Errors is the "errors" array that Meraki sometimes returns inside a JSON body.
	Errors []string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("meraki api %s: HTTP %d", e.Endpoint, e.Status)
	}
	return fmt.Sprintf("meraki api %s: HTTP %d: %s", e.Endpoint, e.Status, e.Message)
}

// UnauthorizedError is returned for HTTP 401 responses. Usually signals a bad or revoked API key.
type UnauthorizedError struct {
	APIError
}

func (e *UnauthorizedError) Error() string {
	return "unauthorized: " + e.APIError.Error()
}

// NotFoundError is returned for HTTP 404 responses.
type NotFoundError struct {
	APIError
}

func (e *NotFoundError) Error() string {
	return "not found: " + e.APIError.Error()
}

// RateLimitError is returned when the client could not satisfy a request within the configured
// retry budget. The RetryAfter value reflects the last Retry-After header seen from Meraki.
type RateLimitError struct {
	APIError
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited: %s (last retry-after=%s)", e.APIError.Error(), e.RetryAfter)
}

// ServerError is returned for unrecoverable 5xx responses (after retries have been exhausted).
type ServerError struct {
	APIError
}

func (e *ServerError) Error() string {
	return "server error: " + e.APIError.Error()
}

// PartialSuccessError is returned when a 2xx response body contains an "errors" field — some
// Meraki endpoints return partial successes this way rather than a 4xx/5xx status.
type PartialSuccessError struct {
	APIError
}

func (e *PartialSuccessError) Error() string {
	return "partial success: " + e.APIError.Error()
}

// IsUnauthorized reports whether err is, or wraps, an UnauthorizedError.
func IsUnauthorized(err error) bool {
	var u *UnauthorizedError
	return errors.As(err, &u)
}

// IsNotFound reports whether err is, or wraps, a NotFoundError.
func IsNotFound(err error) bool {
	var n *NotFoundError
	return errors.As(err, &n)
}

// IsRateLimit reports whether err is, or wraps, a RateLimitError.
func IsRateLimit(err error) bool {
	var r *RateLimitError
	return errors.As(err, &r)
}

// statusToError converts an HTTP status + body into the appropriate typed error.
// retryAfter is only used for 429 responses and may be zero when the header was absent.
func statusToError(endpoint, message string, status int, errorsList []string, retryAfter time.Duration) error {
	base := APIError{Status: status, Endpoint: endpoint, Message: message, Errors: errorsList}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &UnauthorizedError{APIError: base}
	case http.StatusNotFound:
		return &NotFoundError{APIError: base}
	case http.StatusTooManyRequests:
		return &RateLimitError{APIError: base, RetryAfter: retryAfter}
	default:
		if status >= 500 {
			return &ServerError{APIError: base}
		}
		return &base
	}
}
