package crawler

import (
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds configuration for the web crawler.
type Config struct {
	// Solr settings
	SolrURL     string        `env:"SOLR_URL" envDefault:"http://solr:8983/solr/news"`
	SolrTimeout time.Duration `env:"SOLR_TIMEOUT" envDefault:"10s"`

	// Crawl settings
	CrawlDepth        int           `env:"CRAWL_DEPTH" envDefault:"2"`
	CrawlRateLimitRPS float64       `env:"CRAWL_RATE_LIMIT_RPS" envDefault:"2"`
	CrawlBatchSize    int           `env:"CRAWL_BATCH_SIZE" envDefault:"10"`
	CrawlClaimTTL     time.Duration `env:"CRAWL_CLAIM_TTL" envDefault:"5m"`
	CrawlUserAgent    string        `env:"CRAWL_USER_AGENT" envDefault:"TelegramDigestBot/1.0"`
	SeedsFile         string        `env:"CRAWL_SEEDS_FILE" envDefault:"/config/seeds.txt"`

	// Health server
	HealthPort int `env:"HEALTH_PORT" envDefault:"8080"`

	// Logging
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}

// Load loads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// LoadSeeds loads seed URLs from the configured seeds file.
func (c *Config) LoadSeeds() ([]string, error) {
	data, err := os.ReadFile(c.SeedsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("read seeds file: %w", err)
	}

	return parseSeeds(string(data)), nil
}

// parseSeeds parses seed URLs from a newline-separated string.
func parseSeeds(content string) []string {
	var seeds []string

	for _, line := range splitLines(content) {
		line = trimLine(line)

		if line == "" || line[0] == '#' {
			continue
		}

		seeds = append(seeds, line)
	}

	return seeds
}

func splitLines(s string) []string {
	var lines []string

	start := 0

	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}

	if start < len(s) {
		lines = append(lines, s[start:])
	}

	return lines
}

func trimLine(s string) string {
	// Trim whitespace and CR
	start := 0

	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
