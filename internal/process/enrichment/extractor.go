package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	fetchTimeout       = 30 * time.Second
	maxContentLength   = 100000
	maxExtractedClaims = 10
	minClaimLength     = 20
	maxClaimLength     = 500
	llmInputLimit      = 5000

	entityTypePerson  = "PERSON"
	entityTypeOrg     = "ORG"
	entityTypeLoc     = "LOC"
	entityTypeMoney   = "MONEY"
	entityTypePercent = "PERCENT"

	errWrapFmtWithCode = "%w: %d"
	httpHeaderContent  = "Content-Type"
	fieldResponse      = "response"
)

var (
	errUnexpectedStatus   = errors.New("unexpected http status")
	errInvalidLLMResponse = errors.New("invalid LLM response format")
)

type Extractor struct {
	httpClient *http.Client
	llmClient  llm.Client
	llmModel   string
	logger     *zerolog.Logger
}

func NewExtractor(logger *zerolog.Logger) *Extractor {
	if logger == nil {
		l := zerolog.Nop()
		logger = &l
	}

	return &Extractor{
		httpClient: &http.Client{
			Timeout: fetchTimeout,
		},
		logger: logger,
	}
}

// SetLLMClient enables optional LLM claim extraction.
func (e *Extractor) SetLLMClient(client llm.Client, model string) {
	e.llmClient = client
	e.llmModel = model
}

type ExtractedEvidence struct {
	Source *db.EvidenceSource
	Claims []ExtractedClaim
}

type ExtractedClaim struct {
	Text     string   `json:"text"`
	Entities []Entity `json:"entities"`
}

type Entity struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

func (e *Extractor) Extract(ctx context.Context, result SearchResult, provider ProviderName, cacheTTL time.Duration) (*ExtractedEvidence, error) {
	source := &db.EvidenceSource{
		URL:         result.URL,
		URLHash:     db.URLHash(result.URL),
		Domain:      result.Domain,
		Title:       result.Title,
		Description: result.Description,
		PublishedAt: toTimePtr(result.PublishedAt),
		Provider:    string(provider),
		FetchedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(cacheTTL),
	}

	htmlBytes, err := e.fetchContent(ctx, result.URL)
	if err != nil {
		source.ExtractionFailed = true

		//nolint:nilerr // we want to store failed extraction in DB but continue processing
		return &ExtractedEvidence{
			Source: source,
			Claims: nil,
		}, nil
	}

	content, err := links.ExtractWebContent(htmlBytes, result.URL, maxContentLength)
	if err != nil {
		source.ExtractionFailed = true

		//nolint:nilerr // we want to store failed extraction in DB but continue processing
		return &ExtractedEvidence{
			Source: source,
			Claims: nil,
		}, nil
	}

	source.Title = coalesce(content.Title, result.Title)
	source.Description = coalesce(content.Description, result.Description)
	source.Content = content.Content
	source.Author = content.Author
	source.PublishedAt = toTimePtr(coalesce2(content.PublishedAt, result.PublishedAt))
	source.Language = content.Language

	analysisText := content.Content
	if analysisText == "" {
		analysisText = content.Description
	}

	var claims []ExtractedClaim
	if e.llmClient != nil {
		claims, err = e.extractClaimsWithLLM(ctx, analysisText)
		if err != nil {
			e.logger.Error().Err(err).Str("url", result.URL).Msg("LLM extraction failed")

			claims = extractClaims(analysisText)
		}
	} else {
		claims = extractClaims(analysisText)
	}

	return &ExtractedEvidence{
		Source: source,
		Claims: claims,
	}, nil
}

func (e *Extractor) extractClaimsWithLLM(ctx context.Context, content string) ([]ExtractedClaim, error) {
	if e.llmClient == nil {
		return nil, nil
	}

	content = strings.TrimSpace(content)
	if len(content) < 50 {
		return nil, nil
	}

	prompt := `Extract the most significant factual claims from the following text. 
Return a JSON array of objects, where each object has:
- "text": the claim text (single sentence)
- "entities": an array of objects with "text" and "type" (PERSON, ORG, LOC, MONEY, PERCENT)

Text:
` + truncateText(content, llmInputLimit)

	res, err := e.llmClient.CompleteText(ctx, prompt, e.llmModel)
	if err != nil {
		return nil, fmt.Errorf("llm extract claims: %w", err)
	}

	var lastErr error

	var foundValidArray bool

	// Try to find the valid JSON array by trying all combinations of [ and ] positions.
	// This handles cases where the LLM might include preamble or postamble text with brackets.

	for start := strings.Index(res, "["); start != -1; {
		for end := strings.LastIndex(res, "]"); end > start; end = strings.LastIndex(res[:end], "]") {
			var currentClaims []ExtractedClaim
			if err := json.Unmarshal([]byte(res[start:end+1]), &currentClaims); err == nil {
				if len(currentClaims) > 0 {
					return currentClaims, nil
				}

				foundValidArray = true

				continue
			}

			lastErr = err
		}

		// Move to next possible start position
		nextStart := strings.Index(res[start+1:], "[")
		if nextStart == -1 {
			break
		}

		start = start + 1 + nextStart
	}

	if foundValidArray {
		return nil, nil
	}

	if lastErr == nil {
		e.logger.Warn().Str(fieldResponse, res).Msg("invalid LLM response format: no JSON array found")

		return nil, errInvalidLLMResponse
	}

	e.logger.Warn().Err(lastErr).Str(fieldResponse, res).Msg("unmarshal llm claims failed")

	return nil, fmt.Errorf("unmarshal llm claims: %w", lastErr)
}

func toTimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
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

type claimCandidate struct {
	text     string
	score    float64
	entities []Entity
}

func extractClaims(content string) []ExtractedClaim {
	if content == "" {
		return nil
	}

	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return nil
	}

	candidates := filterCandidates(sentences)
	if len(candidates) == 0 {
		return nil
	}

	return rankAndExtractClaims(candidates)
}

func filterCandidates(sentences []string) []claimCandidate {
	candidates := []claimCandidate{}

	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) < minClaimLength || len(s) > maxClaimLength {
			continue
		}

		if !isFactualSentence(s) {
			continue
		}

		entities := extractEntities(s)
		candidates = append(candidates, claimCandidate{
			text:     s,
			entities: entities,
		})
	}

	return candidates
}

func rankAndExtractClaims(candidates []claimCandidate) []ExtractedClaim {
	// Simple TextRank-like scoring based on word overlap with all other sentences
	for i := range candidates {
		for j := range candidates {
			if i == j {
				continue
			}

			candidates[i].score += calculateSentenceSimilarity(candidates[i].text, candidates[j].text)
		}
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	result := []ExtractedClaim{}

	limit := maxExtractedClaims
	if len(candidates) < limit {
		limit = len(candidates)
	}

	for i := 0; i < limit; i++ {
		result = append(result, ExtractedClaim{
			Text:     candidates[i].text,
			Entities: candidates[i].entities,
		})
	}

	return result
}

func calculateSentenceSimilarity(s1, s2 string) float64 {
	words1 := strings.Fields(strings.ToLower(s1))
	words2 := strings.Fields(strings.ToLower(s2))

	if len(words1) == 0 || len(words2) == 0 {
		return 0
	}

	set1 := make(map[string]bool)

	for _, w := range words1 {
		if len(w) > 3 {
			set1[w] = true
		}
	}

	matches := 0

	for _, w := range words2 {
		if set1[w] {
			matches++
		}
	}

	// Normalizing by log of lengths to avoid bias towards long sentences
	return float64(matches) / (math.Log(float64(len(words1))) + math.Log(float64(len(words2))) + 1)
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
		"сообщил", "заявил", "объявил", "подтвердил",
		"согласно", "опубликовал", "выпустил", "говорится",
		"миллион", "миллиард", "процент", "вырос", "снизился",
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
	orgPattern     = regexp.MustCompile(`(?i)(Inc|Corp|Ltd|LLC|Company|Group|Organization|Association|Foundation|ООО|ОАО|ЗАО|ПАО|Группа|Компания|Организация|Фонд)`)
	personPattern  = regexp.MustCompile(`[A-ZА-Я][a-zа-я]+\s+[A-ZА-Я][a-zа-я]+`)
	locPattern     = regexp.MustCompile(`(?i)(United States|Russia|China|Ukraine|Germany|France|UK|USA|Moscow|Washington|Beijing|London|Paris|Berlin|США|Росси[ияюе]|Кита[еяй]|Украин[аыеу]|Германи[ияюе]|Франци[ияюе]|Великобритани[ияюе]|Москв[аыеу]|Вашингтон[ае]?|Пекин[ае]?|Лондон[ае]?|Париж[ае]?|Берлин[ае]?)`)
	moneyPattern   = regexp.MustCompile(`\$[\d,.]+\s*(million|billion|trillion)?|\d+\s*(million|billion|trillion)?\s*(dollars|euros|pounds|рублей|долларов|евро)`)
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
