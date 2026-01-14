package links

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ErrTooManyRedirects indicates too many HTTP redirects.
var ErrTooManyRedirects = errors.New("too many redirects")

// ErrHTTPStatusNotOK indicates an HTTP response with a non-200 status code.
var ErrHTTPStatusNotOK = errors.New("HTTP status not OK")

const (
	defaultFetchTimeoutSeconds = 30
	globalLimiterBurst         = 5
	maxBodySizeMB              = 5
	maxBodySizeBytes           = maxBodySizeMB * 1024 * 1024
	domainLimiterRate          = 1
	domainLimiterBurst         = 2
)

type WebFetcher struct {
	client         *http.Client
	globalLimiter  *rate.Limiter
	domainLimiters map[string]*rate.Limiter
	mu             sync.RWMutex
	userAgent      string
}

func NewWebFetcher(rps float64, timeout time.Duration) *WebFetcher {
	if timeout <= 0 {
		timeout = defaultFetchTimeoutSeconds * time.Second
	}

	return &WebFetcher{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= globalLimiterBurst {
					return ErrTooManyRedirects
				}

				return nil
			},
		},
		globalLimiter:  rate.NewLimiter(rate.Limit(rps), globalLimiterBurst),
		domainLimiters: make(map[string]*rate.Limiter),
		userAgent:      "DigestBot/1.0 (News Aggregator)",
	}
}

func (f *WebFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	// Global rate limit
	if err := f.globalLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("global rate limiter wait: %w", err)
	}

	// Per-domain rate limit (1 req/sec per domain)
	domain := f.extractDomain(rawURL)

	domainLimiter := f.getDomainLimiter(domain)
	if err := domainLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("domain rate limiter wait: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrHTTPStatusNotOK, resp.StatusCode)
	}

	// Limit to 5MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySizeBytes))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return body, nil
}

func (f *WebFetcher) getDomainLimiter(domain string) *rate.Limiter {
	f.mu.RLock()
	limiter, exists := f.domainLimiters[domain]
	f.mu.RUnlock()

	if exists {
		return limiter
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Double check
	if limiter, exists := f.domainLimiters[domain]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(domainLimiterRate, domainLimiterBurst) // 1 req/sec per domain
	f.domainLimiters[domain] = limiter

	return limiter
}

func (f *WebFetcher) extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return strings.ToLower(u.Host)
}
