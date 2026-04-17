package meraki

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// Default values used when ClientConfig leaves a field zero.
const (
	DefaultBaseURL     = "https://api.meraki.com/api/v1"
	DefaultUserAgent   = "Grafana-Meraki-App"
	DefaultHTTPTimeout = 30 * time.Second
	DefaultMaxRetries  = 3
)

// ClientConfig parameterizes a Client. Zero values are replaced with sensible defaults. APIKey
// is the only required field.
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	UserAgent   string
	HTTPTimeout time.Duration
	MaxRetries  int
	Transport   http.RoundTripper
	RateLimiter *RateLimiter
	Cache       *TTLCache
	Logger      log.Logger
}

// Client is a thin Meraki Dashboard API client. It handles auth, retries (429 + 5xx with
// exponential backoff), per-org rate limiting, partial-success detection, and optional
// caching of GET responses.
type Client struct {
	http        *http.Client
	baseURL     string
	apiKey      string
	userAgent   string
	maxRetries  int
	rateLimiter *RateLimiter
	cache       *TTLCache
	logger      log.Logger
}

// NewClient constructs a Client. Returns an error only if APIKey is missing.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("meraki: API key is required")
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = DefaultHTTPTimeout
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.DefaultLogger
	}
	return &Client{
		http:        &http.Client{Timeout: cfg.HTTPTimeout, Transport: cfg.Transport},
		baseURL:     baseURL,
		apiKey:      cfg.APIKey,
		userAgent:   ua,
		maxRetries:  cfg.MaxRetries,
		rateLimiter: cfg.RateLimiter,
		cache:       cfg.Cache,
		logger:      logger,
	}, nil
}

// Do executes a single HTTP request (no caching) and returns body + response headers.
// Retries are handled internally for 429 (honoring Retry-After) and 5xx.
//
// If path is absolute (http:// or https://) the baseURL is bypassed — used by Link-header
// pagination to follow next URLs exactly as the server emits them.
func (c *Client) Do(ctx context.Context, method, path, orgID string, params url.Values, body io.Reader) ([]byte, http.Header, error) {
	fullURL := c.resolveURL(path, params)

	var lastRetryAfter time.Duration
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if c.rateLimiter != nil {
			if _, err := c.rateLimiter.Acquire(ctx, orgID); err != nil {
				return nil, nil, err
			}
		}
		// Only GETs are retried; POST/PUT bodies are not re-readable.
		if attempt > 0 && method != http.MethodGet {
			return nil, nil, errors.New("meraki: non-GET retry not supported")
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
		if err != nil {
			return nil, nil, err
		}
		c.setAuthHeaders(req, body != nil)

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < c.maxRetries && ctx.Err() == nil {
				if sleepErr := sleepCtx(ctx, computeBackoff(attempt)); sleepErr != nil {
					return nil, nil, sleepErr
				}
				continue
			}
			return nil, nil, err
		}
		data, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr != nil {
			return nil, nil, rerr
		}

		// 429 — respect Retry-After when present.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			lastRetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			wait := lastRetryAfter
			if wait <= 0 {
				wait = computeBackoff(attempt)
			}
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, nil, err
			}
			continue
		}
		// 5xx — exponential backoff.
		if resp.StatusCode >= 500 && resp.StatusCode < 600 && attempt < c.maxRetries {
			if err := sleepCtx(ctx, computeBackoff(attempt)); err != nil {
				return nil, nil, err
			}
			continue
		}

		// 2xx success, possibly partial.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if isPartialSuccess(data) {
				msg, errs := parseErrorBody(data)
				return data, resp.Header, &PartialSuccessError{APIError: APIError{
					Status: resp.StatusCode, Endpoint: path, Message: msg, Errors: errs,
				}}
			}
			return data, resp.Header, nil
		}

		// Non-2xx terminal.
		msg, errs := parseErrorBody(data)
		return data, resp.Header, statusToError(path, msg, resp.StatusCode, errs, lastRetryAfter)
	}
	return nil, nil, errors.New("meraki: retry budget exhausted")
}

// Get issues a cached GET. If ttl > 0 and a fresh entry exists, Get short-circuits the HTTP
// request. out is populated via json.Unmarshal.
func (c *Client) Get(ctx context.Context, path, orgID string, params url.Values, ttl time.Duration, out any) error {
	if c.cache != nil && ttl > 0 {
		key := CacheKey(orgID, path, urlValuesToMap(params))
		if raw, ok := c.cache.Get(key); ok {
			return json.Unmarshal(raw, out)
		}
	}
	body, _, err := c.Do(ctx, http.MethodGet, path, orgID, params, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("meraki: decode %s: %w", path, err)
	}
	if c.cache != nil && ttl > 0 {
		c.cache.Set(CacheKey(orgID, path, urlValuesToMap(params)), body, ttl)
	}
	return nil
}

// GetAll follows Link rel=next pagination and returns the concatenated JSON array decoded
// into out (which must be a pointer to a slice). Returns truncated=true if MaxPages was hit.
// ttl controls caching of the full concatenated result.
func (c *Client) GetAll(ctx context.Context, path, orgID string, params url.Values, ttl time.Duration, out any) (bool, error) {
	if c.cache != nil && ttl > 0 {
		key := CacheKey(orgID, path, urlValuesToMap(params))
		if raw, ok := c.cache.Get(key); ok {
			return false, json.Unmarshal(raw, out)
		}
	}

	var pages []json.RawMessage
	truncated := false
	nextPath := path
	nextParams := params
	for page := 0; page < MaxPages; page++ {
		body, hdr, err := c.Do(ctx, http.MethodGet, nextPath, orgID, nextParams, nil)
		if err != nil {
			return false, err
		}
		pages = append(pages, body)
		next := nextLink(hdr)
		if next == "" {
			truncated = false
			break
		}
		nextPath = next
		nextParams = nil // the next URL already has its query string.
		if page == MaxPages-1 {
			truncated = true
		}
	}

	combined, err := mergeJSONArrays(pages)
	if err != nil {
		return truncated, fmt.Errorf("meraki: merge pages for %s: %w", path, err)
	}
	if err := json.Unmarshal(combined, out); err != nil {
		return truncated, fmt.Errorf("meraki: decode %s: %w", path, err)
	}
	if c.cache != nil && ttl > 0 && !truncated {
		c.cache.Set(CacheKey(orgID, path, urlValuesToMap(params)), combined, ttl)
	}
	return truncated, nil
}

func (c *Client) resolveURL(path string, params url.Values) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) setAuthHeaders(req *http.Request, hasBody bool) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Cisco-Meraki-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

func urlValuesToMap(v url.Values) map[string]string {
	m := make(map[string]string, len(v))
	for k, vs := range v {
		if len(vs) > 0 {
			m[k] = vs[0]
		}
	}
	return m
}

// mergeJSONArrays concatenates JSON arrays from each page. Panics if a page is not an array.
func mergeJSONArrays(pages []json.RawMessage) ([]byte, error) {
	if len(pages) == 1 {
		return pages[0], nil
	}
	var merged []json.RawMessage
	for i, p := range pages {
		var arr []json.RawMessage
		if err := json.Unmarshal(p, &arr); err != nil {
			return nil, fmt.Errorf("page %d is not a JSON array: %w", i, err)
		}
		merged = append(merged, arr...)
	}
	return json.Marshal(merged)
}

func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if sec, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && sec >= 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		return time.Until(t)
	}
	return 0
}

func computeBackoff(attempt int) time.Duration {
	base := 250 * time.Millisecond
	shift := base << uint(attempt)
	if shift > 5*time.Second {
		shift = 5 * time.Second
	}
	return shift
}

type metaFields struct {
	Errors []string `json:"errors"`
	Err    string   `json:"error"`
	Msg    string   `json:"message"`
}

func isPartialSuccess(body []byte) bool {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		return false
	}
	var m metaFields
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	return len(m.Errors) > 0
}

func parseErrorBody(body []byte) (string, []string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "", nil
	}
	if body[0] != '{' {
		return string(body), nil
	}
	var m metaFields
	if err := json.Unmarshal(body, &m); err != nil {
		return string(body), nil
	}
	if m.Err != "" {
		return m.Err, m.Errors
	}
	if m.Msg != "" {
		return m.Msg, m.Errors
	}
	if len(m.Errors) > 0 {
		return strings.Join(m.Errors, "; "), m.Errors
	}
	return string(body), nil
}
