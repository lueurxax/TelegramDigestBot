package enrichment

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

const (
	maxQueries          = 4
	minQueryLength      = 10
	maxQueryLength      = 150
	minKeywordLength    = 3
	maxKeywordsPerQuery = 5

	// Language codes
	langEnglish   = "en"
	langRussian   = "ru"
	langUkrainian = "uk"
	langGreek     = "el"
	langUnknown   = "unknown"

	// Language detection thresholds
	cyrillicThreshold = 0.3 // If >30% Cyrillic, consider Russian
	latinThreshold    = 0.5 // If >50% Latin, consider Latin-based language
	greekThreshold    = 0.2 // If >20% Greek, consider Greek

	englishStopwordMin   = 1
	englishStopwordRatio = 0.08
)

// QueryGenerator creates search queries from item summaries.
type QueryGenerator struct{}

// NewQueryGenerator creates a new query generator.
func NewQueryGenerator() *QueryGenerator {
	return &QueryGenerator{}
}

// GeneratedQuery represents a generated search query with metadata.
type GeneratedQuery struct {
	Query    string
	Strategy string // "entity", "keyword", "topic", "fallback"
	Language string // detected language code
}

// queryBuilder helps build and deduplicate generated queries.
type queryBuilder struct {
	queries  []GeneratedQuery
	seen     map[string]bool
	language string
}

func newQueryBuilder(language string) *queryBuilder {
	return &queryBuilder{
		queries:  make([]GeneratedQuery, 0, maxQueries),
		seen:     make(map[string]bool),
		language: language,
	}
}

func (qb *queryBuilder) add(query, strategy string) {
	if query == "" {
		return
	}

	lower := strings.ToLower(query)
	if qb.seen[lower] {
		return
	}

	qb.seen[lower] = true
	qb.queries = append(qb.queries, GeneratedQuery{
		Query:    query,
		Strategy: strategy,
		Language: qb.language,
	})
}

// Generate creates 2-4 search queries from an item summary.
// Algorithm based on proposal:
// - Q1: primary_entity + verb + object
// - Q2: primary_entity + location + date/time
// - Q3: topic + primary_entity + keyword
// - Fallback: top keywords if extraction fails.
func (g *QueryGenerator) Generate(summary, topic, channelTitle string, links []domain.ResolvedLink) []GeneratedQuery {
	if summary == "" {
		return nil
	}

	cleaned := cleanText(summary)
	if len(cleaned) < minQueryLength {
		return nil
	}

	language := detectLanguage(cleaned)
	entities := extractQueryEntities(cleaned)
	locations := extractLocations(cleaned)
	keywords := extractKeywords(cleaned)

	// If summary is vague, pull more entities/keywords from links
	if len(cleaned) < 100 || (len(entities) == 0 && len(locations) == 0) {
		for _, link := range links {
			linkText := cleanText(link.Title + ". " + link.Content)
			if len(linkText) < minQueryLength {
				continue
			}

			entities = append(entities, extractQueryEntities(linkText)...)
			locations = append(locations, extractLocations(linkText)...)
			keywords = append(keywords, extractKeywords(linkText)...)
		}

		// Deduplicate merged entities/locations
		entities = uniqueStrings(entities)
		locations = uniqueStrings(locations)
		keywords = uniqueStrings(keywords)
	}

	qb := newQueryBuilder(language)

	g.addEntityQuery(qb, entities, keywords)
	g.addLocationQuery(qb, entities, locations, keywords)
	g.addTopicQuery(qb, topic, entities, keywords)
	g.addKeywordQuery(qb, keywords)
	g.addFallbackQuery(qb, cleaned, channelTitle, keywords)

	return qb.queries
}

func uniqueStrings(s []string) []string {
	if len(s) == 0 {
		return s
	}

	m := make(map[string]bool)
	res := make([]string, 0, len(s))

	for _, v := range s {
		if !m[v] {
			m[v] = true
			res = append(res, v)
		}
	}

	return res
}

// DetectLanguage returns the detected language code for a text.
func (g *QueryGenerator) DetectLanguage(text string) string {
	return detectLanguage(text)
}

func (g *QueryGenerator) addEntityQuery(qb *queryBuilder, entities, keywords []string) {
	if len(entities) > 0 {
		qb.add(buildEntityQuery(entities[0], keywords), "entity")
	}
}

func (g *QueryGenerator) addLocationQuery(qb *queryBuilder, entities, locations, keywords []string) {
	if len(entities) > 0 && len(locations) > 0 {
		qb.add(buildLocationQuery(entities[0], locations[0], keywords), "location")
	}
}

func (g *QueryGenerator) addTopicQuery(qb *queryBuilder, topic string, entities, keywords []string) {
	if topic != "" && len(keywords) > 0 {
		qb.add(buildTopicQuery(topic, entities, keywords), "topic")
	}
}

func (g *QueryGenerator) addKeywordQuery(qb *queryBuilder, keywords []string) {
	if len(qb.queries) < maxQueries && len(keywords) >= 2 {
		qb.add(buildKeywordQuery(keywords), "keyword")
	}
}

func (g *QueryGenerator) addFallbackQuery(qb *queryBuilder, cleaned, channelTitle string, keywords []string) {
	if len(qb.queries) == 0 {
		query := TruncateQuery(cleaned)

		if channelTitle != "" {
			if len(keywords) > 0 {
				query = channelTitle + " " + strings.Join(keywords, " ")
			} else {
				query = channelTitle + " " + query
			}
		}

		qb.add(TruncateQuery(query), "fallback")
	}
}

// cleanText removes emojis, mentions, hashtags, and normalizes whitespace.
func cleanText(text string) string {
	// Remove emojis
	text = removeEmojis(text)

	// Remove mentions (@username)
	mentionRegex := regexp.MustCompile(`@\w+`)
	text = mentionRegex.ReplaceAllString(text, "")

	// Remove hashtags but keep the word
	hashtagRegex := regexp.MustCompile(`#(\w+)`)
	text = hashtagRegex.ReplaceAllString(text, "$1")

	// Remove URLs
	urlRegex := regexp.MustCompile(`https?://\S+`)
	text = urlRegex.ReplaceAllString(text, "")

	// Normalize whitespace
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// removeEmojis removes emoji characters from text.
func removeEmojis(text string) string {
	var result strings.Builder

	for _, r := range text {
		// Skip emoji ranges
		if isEmoji(r) {
			continue
		}

		result.WriteRune(r)
	}

	return result.String()
}

// emojiRange represents a Unicode range for emoji detection.
type emojiRange struct {
	start, end rune
}

// emojiRanges contains common emoji Unicode ranges.
var emojiRanges = []emojiRange{
	{0x1F300, 0x1F9FF}, // Misc Symbols, Emoticons, etc.
	{0x2600, 0x26FF},   // Misc Symbols
	{0x2700, 0x27BF},   // Dingbats
	{0xFE00, 0xFE0F},   // Variation Selectors
	{0x1F000, 0x1F02F}, // Mahjong, Domino
	{0x1F0A0, 0x1F0FF}, // Playing Cards
}

// isEmoji checks if a rune is an emoji.
func isEmoji(r rune) bool {
	for _, er := range emojiRanges {
		if r >= er.start && r <= er.end {
			return true
		}
	}

	return false
}

var (
	// Patterns for query entity extraction (more comprehensive than extractor patterns)
	qgPersonPattern   = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+\b`)
	qgOrgPattern      = regexp.MustCompile(`\b[A-Z][A-Za-z]*(?:\s+[A-Z][A-Za-z]*)*(?:\s+(?:Inc|Corp|Ltd|LLC|Company|Group|Organization|Association|Foundation|Ministry|Agency|Department|Bank|University))\b`)
	qgAcronymPattern  = regexp.MustCompile(`\b[A-Z]{2,6}\b`)
	qgQuotedPattern   = regexp.MustCompile(`"([^"]+)"`)
	qgLocationPattern = regexp.MustCompile(`(?i)\b(United States|Russia|China|Ukraine|Germany|France|UK|USA|EU|Moscow|Washington|Beijing|London|Paris|Berlin|Kyiv|Kiev|Brussels|Tokyo|New York|California|Texas|Florida)\b`)
)

// extractQueryEntities extracts named entities from text.
func extractQueryEntities(text string) []string {
	entities := make([]string, 0)
	seen := make(map[string]bool)

	addEntity := func(e string) {
		e = strings.TrimSpace(e)
		lower := strings.ToLower(e)

		if len(e) >= minKeywordLength && !seen[lower] && !isStopWord(lower) {
			seen[lower] = true

			entities = append(entities, e)
		}
	}

	// Extract quoted phrases first (often important entities)
	for _, match := range qgQuotedPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			addEntity(match[1])
		}
	}

	// Extract organizations
	for _, match := range qgOrgPattern.FindAllString(text, -1) {
		addEntity(match)
	}

	// Extract person names
	for _, match := range qgPersonPattern.FindAllString(text, -1) {
		addEntity(match)
	}

	// Extract acronyms (often organizations)
	for _, match := range qgAcronymPattern.FindAllString(text, -1) {
		// Skip common non-entity acronyms
		if !isCommonAcronym(match) {
			addEntity(match)
		}
	}

	return entities
}

// extractLocations extracts location names from text.
func extractLocations(text string) []string {
	locations := make([]string, 0)
	seen := make(map[string]bool)

	for _, match := range qgLocationPattern.FindAllString(text, -1) {
		lower := strings.ToLower(match)
		if !seen[lower] {
			seen[lower] = true

			locations = append(locations, match)
		}
	}

	return locations
}

// wordFreq holds a word and its frequency count.
type wordFreq struct {
	word  string
	count int
}

// extractKeywords extracts important keywords from text.
func extractKeywords(text string) []string {
	freq := countWordFrequencies(text)
	sorted := sortByFrequency(freq)

	return selectTopKeywords(sorted)
}

// countWordFrequencies counts word frequencies, filtering out short and stop words.
func countWordFrequencies(text string) map[string]int {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	freq := make(map[string]int)

	for _, word := range words {
		if len(word) >= minKeywordLength && !isStopWord(word) {
			freq[word]++
		}
	}

	return freq
}

// sortByFrequency sorts words by their frequency in descending order.
func sortByFrequency(freq map[string]int) []wordFreq {
	sorted := make([]wordFreq, 0, len(freq))
	for w, c := range freq {
		sorted = append(sorted, wordFreq{w, c})
	}

	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// selectTopKeywords selects the top keywords from a sorted list.
func selectTopKeywords(sorted []wordFreq) []string {
	keywords := make([]string, 0, maxKeywordsPerQuery)

	for _, wf := range sorted {
		keywords = append(keywords, wf.word)

		if len(keywords) >= maxKeywordsPerQuery {
			break
		}
	}

	return keywords
}

// buildEntityQuery creates a query focused on the primary entity.
func buildEntityQuery(entity string, keywords []string) string {
	parts := []string{entity}

	// Add up to 2 keywords for context
	for i := 0; i < 2 && i < len(keywords); i++ {
		if !strings.Contains(strings.ToLower(entity), keywords[i]) {
			parts = append(parts, keywords[i])
		}
	}

	query := strings.Join(parts, " ")

	return TruncateQuery(query)
}

// buildLocationQuery creates a query with entity and location.
func buildLocationQuery(entity, location string, keywords []string) string {
	parts := []string{entity, location}

	// Add one keyword if available
	for _, kw := range keywords {
		if !strings.Contains(strings.ToLower(entity), kw) &&
			!strings.Contains(strings.ToLower(location), kw) {
			parts = append(parts, kw)

			break
		}
	}

	query := strings.Join(parts, " ")

	return TruncateQuery(query)
}

// buildTopicQuery creates a query with topic context.
func buildTopicQuery(topic string, entities, keywords []string) string {
	parts := []string{topic}

	if len(entities) > 0 {
		parts = append(parts, entities[0])
	}

	// Add a keyword
	for _, kw := range keywords {
		lower := strings.ToLower(kw)
		if !strings.Contains(strings.ToLower(topic), lower) {
			parts = append(parts, kw)

			break
		}
	}

	query := strings.Join(parts, " ")

	return TruncateQuery(query)
}

// buildKeywordQuery creates a query from top keywords.
func buildKeywordQuery(keywords []string) string {
	limit := maxKeywordsPerQuery
	if len(keywords) < limit {
		limit = len(keywords)
	}

	query := strings.Join(keywords[:limit], " ")

	return TruncateQuery(query)
}

// TruncateQuery ensures the query doesn't exceed maxQueryLength.
func TruncateQuery(query string) string {
	query = strings.TrimSpace(query)

	if len(query) > maxQueryLength {
		// Truncate at word boundary
		query = query[:maxQueryLength]

		if idx := strings.LastIndex(query, " "); idx > minQueryLength {
			query = query[:idx]
		}
	}

	if len(query) < minQueryLength {
		return ""
	}

	return query
}

// isCommonAcronym checks if an acronym is a common non-entity word.
func isCommonAcronym(s string) bool {
	common := map[string]bool{
		"AM": true, "PM": true, "TV": true, "OK": true,
		"US": true, "UK": true, "EU": true, // These are locations, keep them
		"AI": true, "IT": true, "PR": true, "HR": true,
		"CEO": true, "CFO": true, "CTO": true, "COO": true,
	}

	return common[s]
}

// detectLanguage detects the primary language of text using character analysis.
// Returns "en" for English, "ru" for Russian, "uk" for Ukrainian, "el" for Greek, or "unknown" for other languages.
func detectLanguage(text string) string {
	if text == "" {
		return langUnknown
	}

	latinCount, cyrillicCount, greekCount, totalLetters, hasUkrainian := countCharacters(text)

	if totalLetters == 0 {
		return langUnknown
	}

	cyrillicRatio := float64(cyrillicCount) / float64(totalLetters)
	latinRatio := float64(latinCount) / float64(totalLetters)
	greekRatio := float64(greekCount) / float64(totalLetters)

	if cyrillicRatio >= cyrillicThreshold {
		if hasUkrainian {
			return langUkrainian
		}

		return langRussian
	}

	if greekRatio >= greekThreshold {
		return langGreek
	}

	if latinRatio >= latinThreshold {
		if isLikelyEnglish(text) {
			return langEnglish
		}

		return langUnknown
	}

	return langUnknown
}

func countCharacters(text string) (latinCount, cyrillicCount, greekCount, totalLetters int, hasUkrainian bool) {
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}

		totalLetters++

		if isCyrillic(r) {
			cyrillicCount++

			if isUkrainianLetter(r) {
				hasUkrainian = true
			}
		} else if isGreek(r) {
			greekCount++
		} else if isLatin(r) {
			latinCount++
		}
	}

	return
}

// isCyrillic checks if a rune is a Cyrillic character.
func isCyrillic(r rune) bool {
	return (r >= 0x0400 && r <= 0x04FF) || // Cyrillic
		(r >= 0x0500 && r <= 0x052F) // Cyrillic Supplement
}

// isLatin checks if a rune is a Latin character.
func isLatin(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
		(r >= 0x00C0 && r <= 0x00FF) || // Latin-1 Supplement
		(r >= 0x0100 && r <= 0x017F) // Latin Extended-A
}

func isGreek(r rune) bool {
	return (r >= 0x0370 && r <= 0x03FF) || // Greek and Coptic
		(r >= 0x1F00 && r <= 0x1FFF) // Greek Extended
}

func isUkrainianLetter(r rune) bool {
	switch r {
	case 'і', 'ї', 'є', 'ґ', 'І', 'Ї', 'Є', 'Ґ':
		return true
	default:
		return false
	}
}

// isEnglish checks if the detected language is English.
func isEnglish(language string) bool {
	return language == langEnglish
}

func isLikelyEnglish(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})

	if len(words) == 0 {
		return false
	}

	matches := 0

	for _, w := range words {
		if isStopWord(w) {
			matches++
		}
	}

	if matches < englishStopwordMin {
		return false
	}

	return float64(matches)/float64(len(words)) >= englishStopwordRatio
}
