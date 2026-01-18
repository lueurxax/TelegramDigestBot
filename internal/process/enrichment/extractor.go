package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	fetchTimeout       = 30 * time.Second
	maxContentLength   = 100000
	maxExtractedClaims = 10
	minClaimLength     = 20
	maxClaimLength     = 500

	entityTypePerson  = "PERSON"
	entityTypeOrg     = "ORG"
	entityTypeLoc     = "LOC"
	entityTypeMoney   = "MONEY"
	entityTypePercent = "PERCENT"

	errWrapFmtWithCode = "%w: %d"
)

var errUnexpectedStatus = errors.New("unexpected http status")

type Extractor struct {
	httpClient *http.Client
}

func NewExtractor() *Extractor {
	return &Extractor{
		httpClient: &http.Client{
			Timeout: fetchTimeout,
		},
	}
}

type ExtractedEvidence struct {
	Source *db.EvidenceSource
	Claims []ExtractedClaim
}

type ExtractedClaim struct {
	Text     string
	Entities []Entity
}

type Entity struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

func (e *Extractor) Extract(ctx context.Context, result SearchResult, provider ProviderName, cacheTTL time.Duration) (*ExtractedEvidence, error) {
	htmlBytes, err := e.fetchContent(ctx, result.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch content: %w", err)
	}

	content, err := links.ExtractWebContent(htmlBytes, result.URL, maxContentLength)
	if err != nil {
		return nil, fmt.Errorf("extract web content: %w", err)
	}

	source := &db.EvidenceSource{
		URL:         result.URL,
		URLHash:     db.URLHash(result.URL),
		Domain:      result.Domain,
		Title:       coalesce(content.Title, result.Title),
		Description: coalesce(content.Description, result.Description),
		Content:     content.Content,
		Author:      content.Author,
		PublishedAt: coalesce2(content.PublishedAt, result.PublishedAt),
		Language:    content.Language,
		Provider:    string(provider),
		FetchedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(cacheTTL),
	}

	claims := extractClaims(content.Content)

	return &ExtractedEvidence{
		Source: source,
		Claims: claims,
	}, nil
}

func (e *Extractor) fetchContent(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; DigestBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentLength))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}

func extractClaims(content string) []ExtractedClaim {
	if content == "" {
		return nil
	}

	sentences := splitSentences(content)
	claims := []ExtractedClaim{}

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if len(sentence) < minClaimLength || len(sentence) > maxClaimLength {
			continue
		}

		if !isFactualSentence(sentence) {
			continue
		}

		entities := extractEntities(sentence)
		claims = append(claims, ExtractedClaim{
			Text:     sentence,
			Entities: entities,
		})

		if len(claims) >= maxExtractedClaims {
			break
		}
	}

	return claims
}

var sentenceEndRegex = regexp.MustCompile(`[.!?]+\s+`)

func splitSentences(text string) []string {
	parts := sentenceEndRegex.Split(text, -1)
	sentences := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			sentences = append(sentences, part)
		}
	}

	return sentences
}

func isFactualSentence(sentence string) bool {
	lower := strings.ToLower(sentence)

	factualIndicators := []string{
		"according to", "reported", "announced", "confirmed",
		"said", "stated", "released", "published",
		"million", "billion", "percent", "%",
		"increased", "decreased", "grew", "fell",
	}

	for _, indicator := range factualIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	return containsNumber(sentence)
}

func containsNumber(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}

	return false
}

var (
	orgPattern     = regexp.MustCompile(`(?i)\b(Inc|Corp|Ltd|LLC|Company|Group|Organization|Association|Foundation)\b`)
	personPattern  = regexp.MustCompile(`\b[A-Z][a-z]+\s+[A-Z][a-z]+\b`)
	locPattern     = regexp.MustCompile(`(?i)\b(United States|Russia|China|Ukraine|Germany|France|UK|USA|Moscow|Washington|Beijing|London|Paris|Berlin)\b`)
	moneyPattern   = regexp.MustCompile(`\$[\d,.]+\s*(million|billion|trillion)?|\d+\s*(million|billion|trillion)?\s*(dollars|euros|pounds)`)
	percentPattern = regexp.MustCompile(`\d+(?:\.\d+)?%`)
)

func extractEntities(text string) []Entity {
	entities := []Entity{}
	seen := make(map[string]bool)

	addEntity := func(text, typ string) {
		key := typ + ":" + text
		if !seen[key] {
			seen[key] = true

			entities = append(entities, Entity{Text: text, Type: typ})
		}
	}

	for _, match := range personPattern.FindAllString(text, -1) {
		addEntity(match, entityTypePerson)
	}

	for _, match := range orgPattern.FindAllString(text, -1) {
		addEntity(match, entityTypeOrg)
	}

	for _, match := range locPattern.FindAllString(text, -1) {
		addEntity(match, entityTypeLoc)
	}

	for _, match := range moneyPattern.FindAllString(text, -1) {
		addEntity(match, entityTypeMoney)
	}

	for _, match := range percentPattern.FindAllString(text, -1) {
		addEntity(match, entityTypePercent)
	}

	return entities
}

func (c ExtractedClaim) EntitiesJSON() []byte {
	if len(c.Entities) == 0 {
		return nil
	}

	data, _ := json.Marshal(c.Entities)

	return data
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

func coalesce2(a, b time.Time) time.Time {
	if !a.IsZero() {
		return a
	}

	return b
}
