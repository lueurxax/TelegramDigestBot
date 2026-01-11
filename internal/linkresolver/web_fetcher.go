package linkresolver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
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
		timeout = 30 * time.Second
	}
	return &WebFetcher{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		globalLimiter:  rate.NewLimiter(rate.Limit(rps), 5),
		domainLimiters: make(map[string]*rate.Limiter),
		userAgent:      "DigestBot/1.0 (News Aggregator)",
	}
}

func (f *WebFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	// Global rate limit
	if err := f.globalLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	// Per-domain rate limit (1 req/sec per domain)
	domain := f.extractDomain(rawURL)
	domainLimiter := f.getDomainLimiter(domain)
	if err := domainLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Limit to 5MB
	return io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
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

	limiter = rate.NewLimiter(1, 2) // 1 req/sec per domain
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
