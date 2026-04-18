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
	"golang.org/x/sync/singleflight"
)

// Default values used when ClientConfig leaves a field zero.
//
// DefaultUserAgent matches Meraki's documented format — "ApplicationName/Version VendorName"
// with no spaces/hyphens/special characters in the names. Sourced from BuildUserAgent() so
// version bumps happen in exactly one place (pkg/meraki/version.go).
var DefaultUserAgent = BuildUserAgent()

const (
	DefaultBaseURL     = "https://api.meraki.com/api/v1"
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
	// IPRateLimiter is an optional second token bucket enforcing Meraki's documented
	// per-source-IP cap (100 rps). Set for multi-tenant deployments where many org keys
	// share the same egress IP — at org-cap saturation the per-IP ceiling can still be
	// breached. nil disables; acquires always run IP-limit-first so the org limiter only
	// charges a token after the IP limit has cleared.
	IPRateLimiter *RateLimiter
	Cache         *TTLCache
	Logger        log.Logger
}

// Client is a thin Meraki Dashboard API client. It handles auth, retries (429 + 5xx with
// exponential backoff), per-org rate limiting, partial-success detection, and optional
// caching of GET responses.
//
// Concurrent callers that request the same cache key collapse to a single HTTP round-trip
// via singleflight — so a dashboard with 20 panels querying the same endpoint fans in to
// one upstream request, not 20. The coalescing key matches the cache key so an in-flight
// request and its cache lookup share a lifecycle.
type Client struct {
	http          *http.Client
	baseURL       string
	apiKey        string
	userAgent     string
	maxRetries    int
	rateLimiter   *RateLimiter
	ipRateLimiter *RateLimiter
	cache         *TTLCache
	logger        log.Logger

	// sf coalesces concurrent cache-miss round-trips and concurrent async SWR refreshes.
	// Keyed on the same sha256 CacheKey used to look up the cache entry.
	sf singleflight.Group
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
		http:          &http.Client{Timeout: cfg.HTTPTimeout, Transport: cfg.Transport},
		baseURL:       baseURL,
		apiKey:        cfg.APIKey,
		userAgent:     ua,
		maxRetries:    cfg.MaxRetries,
		rateLimiter:   cfg.RateLimiter,
		ipRateLimiter: cfg.IPRateLimiter,
		cache:         cfg.Cache,
		logger:        logger,
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
		// IP-level limit first — per Meraki's doc (§7.1 / §7.4-G) the 100 rps per-source-IP
		// cap is independent of the per-org cap. Acquiring IP-first means the org bucket
		// only charges after the IP bucket has room, so we don't over-credit the org budget
		// while actually blocked at the IP layer.
		if c.ipRateLimiter != nil {
			if _, err := c.ipRateLimiter.Acquire(ctx, "ip"); err != nil {
				return nil, nil, err
			}
		}
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
// request. Stale entries (within stale-grace) are served immediately while an async
// refresh is kicked off — panel refreshes stay fast while data gets revalidated behind
// the scenes. Negative-cached 404s short-circuit with a NotFoundError (no round-trip).
// out is populated via json.Unmarshal.
func (c *Client) Get(ctx context.Context, path, orgID string, params url.Values, ttl time.Duration, out any) error {
	paramsMap := urlValuesToMap(params)
	key := CacheKey(orgID, path, paramsMap)
	if c.cache != nil && ttl > 0 {
		r := c.cache.Lookup(orgID, key)
		if r.Hit {
			if r.NotFound {
				return c.negativeCachedNotFound(path)
			}
			if r.Stale {
				c.kickGetRefresh(orgID, key, path, cloneParams(params), ttl)
			}
			return json.Unmarshal(r.Value, out)
		}
	}
	body, err := c.doGetCoalesced(ctx, orgID, key, path, params, ttl)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("meraki: decode %s: %w", path, err)
	}
	return nil
}

// GetAll follows Link rel=next pagination and returns the concatenated JSON array decoded
// into out (which must be a pointer to a slice). Returns truncated=true if MaxPages was hit.
// ttl controls caching of the full concatenated result; truncated walks are not cached so
// a follow-up retry actually re-paginates.
//
// As with Get: concurrent callers collapse to a single multi-page walk via singleflight,
// stale cache hits serve immediately with async refresh, and 404s are negative-cached.
func (c *Client) GetAll(ctx context.Context, path, orgID string, params url.Values, ttl time.Duration, out any) (bool, error) {
	paramsMap := urlValuesToMap(params)
	key := CacheKey(orgID, path, paramsMap)
	if c.cache != nil && ttl > 0 {
		r := c.cache.Lookup(orgID, key)
		if r.Hit {
			if r.NotFound {
				return false, c.negativeCachedNotFound(path)
			}
			if r.Stale {
				c.kickGetAllRefresh(orgID, key, path, cloneParams(params), ttl)
			}
			return false, json.Unmarshal(r.Value, out)
		}
	}

	combined, truncated, err := c.doGetAllCoalesced(ctx, orgID, key, path, params, ttl)
	if err != nil {
		return truncated, err
	}
	if err := json.Unmarshal(combined, out); err != nil {
		return truncated, fmt.Errorf("meraki: decode %s: %w", path, err)
	}
	return truncated, nil
}

// doGetCoalesced runs the HTTP GET under singleflight keyed on the cache key. Concurrent
// cache-miss callers for the same key see exactly one round-trip; success results are
// written to the cache with a TTL/2 (capped at 30m) stale-grace so subsequent panel
// refreshes benefit from SWR semantics.
func (c *Client) doGetCoalesced(ctx context.Context, orgID, key, path string, params url.Values, ttl time.Duration) ([]byte, error) {
	v, err, _ := c.sf.Do(key, func() (any, error) {
		// Re-check the cache under the singleflight: while we waited for the lock a sibling
		// may have populated it. This avoids a second HTTP round-trip.
		if c.cache != nil && ttl > 0 {
			if r := c.cache.Lookup(orgID, key); r.Hit && !r.Stale && !r.NotFound {
				return r.Value, nil
			}
		}
		body, _, err := c.Do(ctx, http.MethodGet, path, orgID, params, nil)
		if err != nil {
			if c.cache != nil && ttl > 0 && IsNotFound(err) {
				c.cache.StoreNotFound(orgID, key, 0)
			}
			return nil, err
		}
		if c.cache != nil && ttl > 0 {
			c.cache.Store(orgID, key, body, ttl, deriveStaleGrace(ttl))
		}
		return body, nil
	})
	if err != nil {
		return nil, err
	}
	// The singleflight return value is whatever fn returned — always []byte here.
	body, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("meraki: singleflight returned unexpected type %T", v)
	}
	return body, nil
}

// doGetAllCoalesced is the multi-page counterpart to doGetCoalesced. The pagination loop
// runs inside the singleflight fn so every joining caller shares the full walk.
func (c *Client) doGetAllCoalesced(ctx context.Context, orgID, key, path string, params url.Values, ttl time.Duration) ([]byte, bool, error) {
	type pagedResult struct {
		body      []byte
		truncated bool
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		if c.cache != nil && ttl > 0 {
			if r := c.cache.Lookup(orgID, key); r.Hit && !r.Stale && !r.NotFound {
				return &pagedResult{body: r.Value, truncated: false}, nil
			}
		}
		combined, truncated, err := c.paginate(ctx, path, orgID, params)
		if err != nil {
			if c.cache != nil && ttl > 0 && IsNotFound(err) {
				c.cache.StoreNotFound(orgID, key, 0)
			}
			return nil, err
		}
		// Truncated walks are not cached — a follow-up request should actually continue
		// where the previous one ran out of pages.
		if c.cache != nil && ttl > 0 && !truncated {
			c.cache.Store(orgID, key, combined, ttl, deriveStaleGrace(ttl))
		}
		return &pagedResult{body: combined, truncated: truncated}, nil
	})
	if err != nil {
		return nil, false, err
	}
	res, ok := v.(*pagedResult)
	if !ok {
		return nil, false, fmt.Errorf("meraki: singleflight returned unexpected type %T", v)
	}
	return res.body, res.truncated, nil
}

// paginate runs the Link-header pagination loop used by GetAll. Extracted so both the
// direct cache-miss path and the async SWR refresh can share it.
func (c *Client) paginate(ctx context.Context, path, orgID string, params url.Values) ([]byte, bool, error) {
	var pages []json.RawMessage
	truncated := false
	nextPath := path
	nextParams := params
	for page := 0; page < MaxPages; page++ {
		body, hdr, err := c.Do(ctx, http.MethodGet, nextPath, orgID, nextParams, nil)
		if err != nil {
			return nil, false, err
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
		return nil, truncated, fmt.Errorf("meraki: merge pages for %s: %w", path, err)
	}
	return combined, truncated, nil
}

// kickGetRefresh fires an async revalidation for a stale cache entry. Runs on a detached
// context with a 30s ceiling so a short-lived panel-refresh ctx cancelling doesn't abort
// the refresh mid-flight. Joined via the same singleflight key — a sibling
// doGetCoalesced already in progress will absorb this call without a second round-trip.
func (c *Client) kickGetRefresh(orgID, key, path string, params url.Values, ttl time.Duration) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _, _ = c.sf.Do(key, func() (any, error) {
			body, _, err := c.Do(ctx, http.MethodGet, path, orgID, params, nil)
			if err != nil {
				if c.cache != nil && ttl > 0 && IsNotFound(err) {
					c.cache.StoreNotFound(orgID, key, 0)
				}
				return nil, err
			}
			if c.cache != nil && ttl > 0 {
				c.cache.Store(orgID, key, body, ttl, deriveStaleGrace(ttl))
			}
			return body, nil
		})
	}()
}

// kickGetAllRefresh is the GetAll counterpart of kickGetRefresh.
func (c *Client) kickGetAllRefresh(orgID, key, path string, params url.Values, ttl time.Duration) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _, _ = c.sf.Do(key, func() (any, error) {
			combined, truncated, err := c.paginate(ctx, path, orgID, params)
			if err != nil {
				if c.cache != nil && ttl > 0 && IsNotFound(err) {
					c.cache.StoreNotFound(orgID, key, 0)
				}
				return nil, err
			}
			if c.cache != nil && ttl > 0 && !truncated {
				c.cache.Store(orgID, key, combined, ttl, deriveStaleGrace(ttl))
			}
			return combined, nil
		})
	}()
}

// negativeCachedNotFound returns a NotFoundError mirroring the one that populated the
// negative cache. We intentionally do NOT surface the original body/errors list — those
// were for a prior request and may mislead the caller.
func (c *Client) negativeCachedNotFound(path string) error {
	return &NotFoundError{APIError: APIError{
		Status:   http.StatusNotFound,
		Endpoint: path,
		Message:  "not found (cached)",
	}}
}

// deriveStaleGrace returns a stale-grace duration proportional to the freshness TTL.
// The heuristic is TTL/2 with an absolute cap of 30m so very long TTLs don't extend the
// serve-stale window beyond what operators expect. This lines up with §7.4-C proposals:
//
//	Organizations (1h):  30m stale  (matches spec)
//	Networks (15m):      7.5m stale (≈ 10m target)
//	Devices (5m):        2.5m stale (≈ 2m target)
//	SensorHistory (1m):  30s stale  (matches spec)
//	Alerts (30s):        15s stale  (matches spec)
func deriveStaleGrace(ttl time.Duration) time.Duration {
	grace := ttl / 2
	if grace > 30*time.Minute {
		grace = 30 * time.Minute
	}
	return grace
}

// cloneParams returns an independent copy of params so the async refresh goroutine can
// safely mutate its own copy without racing the caller. url.Values is a map — aliasing
// would let a Do()-side mutation (none today, but a cheap safety net) leak across
// goroutines.
func cloneParams(src url.Values) url.Values {
	if src == nil {
		return nil
	}
	dst := make(url.Values, len(src))
	for k, vs := range src {
		cp := make([]string, len(vs))
		copy(cp, vs)
		dst[k] = cp
	}
	return dst
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
