package research

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	minClaimLength     = 30
	maxClaimLength     = 300
	maxClaimsPerItem   = 5
	minKeywordOverlap  = 2
	tfidfTopKeywords   = 20
	minWordLength      = 3
	sentenceSplitLimit = 50
	baseIDFValue       = 2.0
	factualBoost       = 1.3
	digitBoost         = 1.2
	questionPenalty    = 0.5
)

// HeuristicClaim represents a claim extracted from item text.
type HeuristicClaim struct {
	Text           string
	NormalizedHash string
	Keywords       []string
	Score          float64
}

// ClaimExtractor extracts claims from text using TF-IDF and sentence overlap.
type ClaimExtractor struct {
	stopWords map[string]bool
}

// NewClaimExtractor creates a new heuristic claim extractor.
func NewClaimExtractor() *ClaimExtractor {
	return &ClaimExtractor{
		stopWords: buildStopWords(),
	}
}

// ExtractClaims extracts claims from item text using TF-IDF + sentence overlap.
func (e *ClaimExtractor) ExtractClaims(text string) []HeuristicClaim {
	if len(text) < minClaimLength {
		return nil
	}

	// Split into sentences
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return nil
	}

	// Calculate TF-IDF for the document
	tfidf := e.calculateTFIDF(text)

	// Get top keywords
	topKeywords := e.getTopKeywords(tfidf, tfidfTopKeywords)
	if len(topKeywords) == 0 {
		return nil
	}

	// Score sentences by keyword overlap
	scoredSentences := e.scoreSentences(sentences, topKeywords)

	// Filter and deduplicate claims
	return e.filterAndDedup(scoredSentences)
}

// calculateTFIDF computes TF-IDF scores for words in the document.
func (e *ClaimExtractor) calculateTFIDF(text string) map[string]float64 {
	words := tokenize(text)
	if len(words) == 0 {
		return nil
	}

	// Calculate term frequency
	tf := make(map[string]int)

	for _, word := range words {
		if e.isValidWord(word) {
			tf[word]++
		}
	}

	// Calculate TF-IDF (simplified: using log frequency weighting)
	// In a full implementation, IDF would come from a corpus
	tfidf := make(map[string]float64)

	maxFreq := 1
	for _, count := range tf {
		if count > maxFreq {
			maxFreq = count
		}
	}

	for word, count := range tf {
		// Normalized TF with log weighting
		normalizedTF := float64(count) / float64(maxFreq)
		// Simplified IDF: penalize very common words (appearing in >50% of sentences)
		idf := math.Log(baseIDFValue)
		tfidf[word] = normalizedTF * idf
	}

	return tfidf
}

// getTopKeywords returns the top N keywords by TF-IDF score.
func (e *ClaimExtractor) getTopKeywords(tfidf map[string]float64, n int) []string {
	type wordScore struct {
		word  string
		score float64
	}

	scores := make([]wordScore, 0, len(tfidf))

	for word, score := range tfidf {
		scores = append(scores, wordScore{word, score})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	result := make([]string, 0, n)
	for i := 0; i < len(scores) && i < n; i++ {
		result = append(result, scores[i].word)
	}

	return result
}

// scoreSentences scores sentences by keyword overlap.
func (e *ClaimExtractor) scoreSentences(sentences []string, keywords []string) []HeuristicClaim {
	keywordSet := make(map[string]bool)
	for _, kw := range keywords {
		keywordSet[kw] = true
	}

	var claims []HeuristicClaim

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if len(sentence) < minClaimLength || len(sentence) > maxClaimLength {
			continue
		}

		// Count keyword overlap
		words := tokenize(sentence)

		var matchedKeywords []string

		for _, word := range words {
			if keywordSet[word] {
				matchedKeywords = append(matchedKeywords, word)
			}
		}

		if len(matchedKeywords) < minKeywordOverlap {
			continue
		}

		// Score = keyword overlap ratio * sentence quality
		overlapScore := float64(len(matchedKeywords)) / float64(len(keywords))
		qualityScore := sentenceQuality(sentence)
		score := overlapScore * qualityScore

		claims = append(claims, HeuristicClaim{
			Text:           sentence,
			NormalizedHash: normalizeAndHash(sentence),
			Keywords:       matchedKeywords,
			Score:          score,
		})
	}

	// Sort by score descending
	sort.Slice(claims, func(i, j int) bool {
		return claims[i].Score > claims[j].Score
	})

	return claims
}

// filterAndDedup filters claims and removes duplicates.
func (e *ClaimExtractor) filterAndDedup(claims []HeuristicClaim) []HeuristicClaim {
	seen := make(map[string]bool)

	var result []HeuristicClaim

	for _, claim := range claims {
		if seen[claim.NormalizedHash] {
			continue
		}

		seen[claim.NormalizedHash] = true
		result = append(result, claim)

		if len(result) >= maxClaimsPerItem {
			break
		}
	}

	return result
}

// isValidWord checks if a word is valid for TF-IDF.
func (e *ClaimExtractor) isValidWord(word string) bool {
	if len(word) < minWordLength {
		return false
	}

	if e.stopWords[word] {
		return false
	}

	// Must contain at least one letter
	for _, r := range word {
		if unicode.IsLetter(r) {
			return true
		}
	}

	return false
}

// sentenceQuality scores sentence quality (favors factual indicators).
func sentenceQuality(sentence string) float64 {
	lower := strings.ToLower(sentence)
	score := 1.0

	// Boost for factual indicators
	factualIndicators := []string{
		"according to", "reported", "announced", "confirmed",
		"said", "stated", "released", "published",
		"million", "billion", "percent", "%",
		"increased", "decreased", "grew", "fell",
		// Russian
		"сообщил", "заявил", "объявил", "подтвердил",
		"согласно", "опубликовал",
		"миллион", "миллиард", "процент",
	}

	for _, indicator := range factualIndicators {
		if strings.Contains(lower, indicator) {
			score *= factualBoost
		}
	}

	// Boost for numbers (often factual)
	if containsDigit(sentence) {
		score *= digitBoost
	}

	// Penalize questions and exclamations
	if strings.HasSuffix(sentence, "?") || strings.HasSuffix(sentence, "!") {
		score *= questionPenalty
	}

	return score
}

// containsDigit checks if string contains a digit.
func containsDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}

	return false
}

// tokenize splits text into lowercase words.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	// Split on non-letter characters
	splitter := func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}

	return strings.FieldsFunc(text, splitter)
}

// splitSentences splits text into sentences.
func splitSentences(text string) []string {
	// Handle common sentence endings
	pattern := regexp.MustCompile(`[.!?]+\s+|[.!?]+$`)
	parts := pattern.Split(text, sentenceSplitLimit)

	var sentences []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			sentences = append(sentences, part)
		}
	}

	return sentences
}

// normalizeAndHash creates a normalized hash for deduplication.
func normalizeAndHash(text string) string {
	// Normalize: lowercase, remove punctuation, collapse whitespace
	normalized := strings.ToLower(text)
	normalized = regexp.MustCompile(`[^\p{L}\p{N}\s]`).ReplaceAllString(normalized, "")
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)

	hash := sha256.Sum256([]byte(normalized))

	return hex.EncodeToString(hash[:16]) // Use first 16 bytes
}

// buildStopWords returns a set of stop words for multiple languages.
func buildStopWords() map[string]bool {
	words := []string{
		// English
		"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for",
		"of", "with", "by", "from", "as", "is", "was", "are", "were", "been",
		"be", "have", "has", "had", "do", "does", "did", "will", "would", "could",
		"should", "may", "might", "must", "shall", "can", "need", "dare", "ought",
		"used", "this", "that", "these", "those", "i", "you", "he", "she", "it",
		"we", "they", "what", "which", "who", "whom", "whose", "where", "when",
		"why", "how", "all", "each", "every", "both", "few", "more", "most",
		"other", "some", "such", "no", "nor", "not", "only", "own", "same",
		"so", "than", "too", "very", "just", "also", "now", "here", "there",
		// Russian
		"и", "в", "во", "не", "что", "он", "на", "я", "с", "со", "как", "а",
		"то", "все", "она", "так", "его", "но", "да", "ты", "к", "у", "же",
		"вы", "за", "бы", "по", "только", "её", "мне", "было", "вот", "от",
		"меня", "ещё", "нет", "о", "из", "ему", "теперь", "когда", "уже",
		"вам", "ним", "здесь", "этот", "эта", "это", "эти", "при", "для",
		// Ukrainian
		"і", "в", "на", "що", "як", "до", "з", "та", "не", "є", "це", "той",
		"за", "від", "про", "але", "його", "її", "їх", "ми", "ви", "вони",
	}

	stopWords := make(map[string]bool)
	for _, w := range words {
		stopWords[w] = true
	}

	return stopWords
}
