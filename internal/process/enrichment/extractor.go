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

	httpHeaderContent = "Content-Type"
	fieldResponse     = "response"
	fieldAttempt      = "attempt"

	// LLM retry settings
	llmMaxRetries       = 2
	llmRetryDelay       = 2 * time.Second
	llmRetryBackoffMult = 2
)

var (
	errUnexpectedStatus   = errors.New("unexpected http status")
	errInvalidLLMResponse = errors.New("invalid LLM response format")
	errNonTextualContent  = errors.New("non-textual content type")
)

const defaultLLMTimeout = 45 * time.Second

type Extractor struct {
	httpClient *http.Client
	llmClient  llm.Client
	llmModel   string
	llmTimeout time.Duration
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

// SetLLMTimeout sets the timeout for LLM extraction calls.
func (e *Extractor) SetLLMTimeout(timeout time.Duration) {
	if timeout > 0 {
		e.llmTimeout = timeout
	}
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
			if errors.Is(err, errInvalidLLMResponse) || strings.Contains(err.Error(), "unmarshal") {
				e.logger.Warn().Err(err).Str(logKeyURL, result.URL).Msg("LLM extraction returned invalid format")
			} else {
				e.logger.Error().Err(err).Str(logKeyURL, result.URL).Msg("LLM extraction failed")
			}

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

	prompt := e.buildClaimExtractionPrompt(content)

	return e.executeWithRetry(ctx, prompt)
}

func (e *Extractor) buildClaimExtractionPrompt(content string) string {
	return `Extract the most significant factual claims from the following text.
Return a JSON array of objects, where each object has:
- "text": the claim text (single sentence)
- "entities": an array of objects with "text" and "type" (PERSON, ORG, LOC, MONEY, PERCENT)

Text:
` + truncateText(content, llmInputLimit)
}

func (e *Extractor) executeWithRetry(ctx context.Context, prompt string) ([]ExtractedClaim, error) {
	var lastErr error

	delay := llmRetryDelay

	for attempt := 0; attempt <= llmMaxRetries; attempt++ {
		if attempt > 0 {
			e.logger.Debug().
				Int(fieldAttempt, attempt+1).
				Dur("delay", delay).
				Msg("retrying LLM extraction after timeout")

			if err := e.sleepWithContext(ctx, delay); err != nil {
				return nil, fmt.Errorf("llm extract claims: retry interrupted: %w", err)
			}

			delay *= llmRetryBackoffMult
		}

		claims, err := e.tryLLMExtraction(ctx, prompt)
		if err == nil {
			return claims, nil
		}

		lastErr = err

		if !isRetryableError(err) {
			return nil, fmt.Errorf("llm extract claims: %w", err)
		}

		e.logger.Warn().
			Err(err).
			Int(fieldAttempt, attempt+1).
			Int("max_retries", llmMaxRetries+1).
			Msg("LLM extraction failed with retryable error")
	}

	return nil, fmt.Errorf("llm extract claims: max retries exceeded: %w", lastErr)
}

func (e *Extractor) tryLLMExtraction(ctx context.Context, prompt string) ([]ExtractedClaim, error) {
	llmCtx, cancel := e.createLLMContext(ctx)
	defer cancel()

	res, err := e.llmClient.CompleteText(llmCtx, prompt, e.llmModel)
	if err != nil {
		return nil, fmt.Errorf("llm completion: %w", err)
	}

	return e.parseLLMClaims(res)
}

func (e *Extractor) sleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context done: %w", ctx.Err())
	case <-time.After(d):
		return nil
	}
}

// isRetryableError checks if the error is retryable (timeout or cancellation).
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	errStr := err.Error()

	return strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context canceled") ||
		strings.Contains(errStr, "timeout")
}

// createLLMContext creates a context with dedicated LLM timeout.
// It uses an independent deadline (not bound by parent's deadline) but still
// respects parent cancellation for graceful shutdown.
func (e *Extractor) createLLMContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := e.llmTimeout
	if timeout <= 0 {
		timeout = defaultLLMTimeout
	}

	// Create a new context with its own deadline, independent of parent's deadline
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// Propagate parent cancellation to the new context
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx, cancel
}

func (e *Extractor) parseLLMClaims(res string) ([]ExtractedClaim, error) {
	var (
		lastErr         error
		foundValidArray bool
	)

	// Try to find the valid JSON array by trying all combinations of [ and ] positions.
	// This handles cases where the LLM might include preamble or postamble text with brackets.
	for start := strings.Index(res, "["); start != -1; {
		for end := strings.LastIndex(res, "]"); end > start; end = strings.LastIndex(res[:end], "]") {
			var currentClaims []ExtractedClaim

			err := json.Unmarshal([]byte(res[start:end+1]), &currentClaims)
			if err == nil {
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
		e.logger.Debug().Str(fieldResponse, truncateText(res, responseTruncateLen)).Msg("invalid LLM response format: no JSON array found")

		return nil, errInvalidLLMResponse
	}

	e.logger.Warn().Err(lastErr).Str(fieldResponse, truncateText(res, responseTruncateLen)).Msg("unmarshal llm claims failed")

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
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")

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

	contentType, err := e.checkResponseContentType(resp)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentLength))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if err = e.detectAndVerifyContentType(body, contentType); err != nil {
		return nil, err
	}

	return body, nil
}

func (e *Extractor) checkResponseContentType(resp *http.Response) (string, error) {
	contentType := resp.Header.Get(httpHeaderContent)
	if contentType != "" &&
		contentType != applicationOctetStream &&
		!isTextualContentType(contentType) {
		return "", fmt.Errorf(fmtErrWrapStr, errNonTextualContent, contentType)
	}

	return contentType, nil
}

func (e *Extractor) detectAndVerifyContentType(body []byte, initialCT string) error {
	detectedType := initialCT
	if detectedType == "" || detectedType == applicationOctetStream {
		detectedType = http.DetectContentType(body)
	}

	if !isTextualContentType(detectedType) {
		return fmt.Errorf(fmtErrWrapStr, errNonTextualContent, detectedType)
	}

	return nil
}

func isTextualContentType(ct string) bool {
	ct = strings.ToLower(ct)

	return strings.HasPrefix(ct, "text/") ||
		strings.Contains(ct, "html") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "json") ||
		strings.Contains(ct, "rss") ||
		strings.Contains(ct, "atom")
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
	personPattern  = regexp.MustCompile(`[A-ZА-ЯЁ][a-zа-яё]+(?:-[A-ZА-ЯЁ][a-zа-яё]+)?(?:\s+[A-ZА-ЯЁ][a-zа-яё]+(?:-[A-ZА-ЯЁ][a-zа-яё]+)?){1,2}`)
	locPattern     = regexp.MustCompile(`(?i)(United States|Russia|China|Ukraine|Germany|France|UK|USA|Moscow|Washington|Beijing|London|Paris|Berlin|США|Росси[ияюе]|Кита[еяй]|Украин[аыеу]|Германи[ияюе]|Франци[ияюе]|Великобритани[ияюе]|Москв[аыеу]|Вашингтон[ае]?|Пекин[ае]?|Лондон[ае]?|Париж[ае]?|Берлин[ае]?|Земл[яеи])`)
	moneyPattern   = regexp.MustCompile(`\$[\d,.]+\s*(million|billion|trillion)?|\d+\s*(million|billion|trillion)?\s*(dollars|euros|pounds|рублей|долларов|евро)`)
	percentPattern = regexp.MustCompile(`\d+(?:\.\d+)?%`)
)

type entityExtractor struct {
	entities []Entity
	seen     map[string]bool
}

func newEntityExtractor() *entityExtractor {
	return &entityExtractor{
		entities: []Entity{},
		seen:     make(map[string]bool),
	}
}

func (e *entityExtractor) add(text, typ string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	key := typ + ":" + text
	if !e.seen[key] {
		e.seen[key] = true
		e.entities = append(e.entities, Entity{Text: text, Type: typ})
	}
}

func (e *entityExtractor) addFromPattern(pattern *regexp.Regexp, text, typ string) {
	for _, match := range pattern.FindAllString(text, -1) {
		e.add(match, typ)
	}
}

func extractEntities(text string) []Entity {
	ext := newEntityExtractor()
	text = normalizeCyrillic(text)

	ext.addFromPattern(personPattern, text, entityTypePerson)
	ext.addFromPattern(orgPattern, text, entityTypeOrg)

	for _, acronym := range extractCyrillicAcronyms(text) {
		ext.add(acronym, entityTypeOrg)
	}

	ext.addFromPattern(locPattern, text, entityTypeLoc)
	ext.addFromPattern(moneyPattern, text, entityTypeMoney)
	ext.addFromPattern(percentPattern, text, entityTypePercent)

	if !hasEntityType(ext.entities, entityTypePerson) {
		for _, phrase := range extractCapitalizedPhrases(text) {
			ext.add(phrase, entityTypePerson)
		}
	}

	return ext.entities
}

func hasEntityType(entities []Entity, typ string) bool {
	for _, e := range entities {
		if e.Type == typ {
			return true
		}
	}

	return false
}

func extractCyrillicAcronyms(text string) []string {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r)
	})

	acronyms := make([]string, 0)
	seen := make(map[string]bool)

	for _, word := range words {
		word = strings.TrimSpace(word)

		if !isCyrillicAcronym(word) {
			continue
		}

		if !seen[word] {
			seen[word] = true
			acronyms = append(acronyms, word)
		}
	}

	return acronyms
}

func isCyrillicAcronym(word string) bool {
	length := runeCount(word)
	if length < 2 || length > 6 {
		return false
	}

	hasCyrillic := false

	for _, r := range word {
		if !unicode.IsLetter(r) || !unicode.IsUpper(r) {
			return false
		}

		if isCyrillicRune(r) {
			hasCyrillic = true
		}
	}

	return hasCyrillic
}

func extractCapitalizedPhrases(text string) []string {
	words := splitWords(text)
	phrases := make([]string, 0)

	for i := 0; i < len(words); i++ {
		if !isTitleCaseWord(words[i]) {
			continue
		}

		phrase := []string{words[i]}
		for j := i + 1; j < len(words) && len(phrase) < 3; j++ {
			if !isTitleCaseWord(words[j]) {
				break
			}

			phrase = append(phrase, words[j])
			i = j
		}

		if len(phrase) >= 2 {
			phrases = append(phrases, strings.Join(phrase, " "))
		}
	}

	return phrases
}

func splitWords(text string) []string {
	fields := strings.Fields(text)
	words := make([]string, 0, len(fields))

	for _, f := range fields {
		word := strings.TrimFunc(f, func(r rune) bool {
			return !unicode.IsLetter(r) && r != '-'
		})

		if word != "" {
			words = append(words, word)
		}
	}

	return words
}

func isTitleCaseWord(word string) bool {
	if word == "" {
		return false
	}

	runes := []rune(word)

	first := runes[0]
	if !unicode.IsUpper(first) {
		return false
	}

	hasLetter := false

	for i := 1; i < len(runes); i++ {
		r := runes[i]
		if r == '-' {
			continue
		}

		if unicode.IsLetter(r) {
			hasLetter = true

			if !unicode.IsLower(r) {
				return false
			}
		}
	}

	return hasLetter
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
