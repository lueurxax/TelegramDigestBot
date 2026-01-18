package enrichment

import (
	"encoding/json"
	"strings"
	"unicode"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	jaccardWeight          = 0.6
	entityWeight           = 0.4
	entityTypeBoost        = 1.5
	minTokenLength         = 3
	highConfidenceMin      = 2
	maxItemClaimLen        = 100
	maxEvidenceClaimLen    = 200
	minMatchScore          = 0.3
	highTierScoreThreshold = 0.5
	contradictionThreshold = 0.4 // Entity overlap threshold for contradiction check
)

type ScoringResult struct {
	AgreementScore  float32
	IsContradiction bool
	MatchedClaims   []MatchedClaim
	Tier            string
}

type MatchedClaim struct {
	ItemClaim     string  `json:"item_claim"`
	EvidenceClaim string  `json:"evidence_claim"`
	Score         float32 `json:"score"`
}

type Scorer struct{}

func NewScorer() *Scorer {
	return &Scorer{}
}

func (s *Scorer) Score(itemSummary string, evidence *ExtractedEvidence) ScoringResult {
	if evidence == nil || len(evidence.Claims) == 0 {
		return ScoringResult{
			AgreementScore: 0,
			Tier:           db.FactCheckTierLow,
		}
	}

	itemTokens := tokenize(itemSummary)
	itemEntities := extractEntities(itemSummary)

	var bestScore float32

	var matchedClaims []MatchedClaim

	for _, claim := range evidence.Claims {
		claimTokens := tokenize(claim.Text)

		jaccardSim := jaccardSimilarity(itemTokens, claimTokens)

		entityOverlap := entityOverlapRatio(itemEntities, claim.Entities)

		score := float32(jaccardWeight*jaccardSim + entityWeight*entityOverlap)

		if score > bestScore {
			bestScore = score
		}

		if score > minMatchScore {
			matchedClaims = append(matchedClaims, MatchedClaim{
				ItemClaim:     truncateString(itemSummary, maxItemClaimLen),
				EvidenceClaim: truncateString(claim.Text, maxEvidenceClaimLen),
				Score:         score,
			})
		}
	}

	// Check for contradiction: high entity overlap but low text similarity may indicate contradiction
	isContradiction := detectContradiction(itemSummary, evidence.Claims, itemEntities)

	return ScoringResult{
		AgreementScore:  bestScore,
		IsContradiction: isContradiction,
		MatchedClaims:   matchedClaims,
	}
}

func (s *Scorer) DetermineTier(sourceCount int, avgScore float32) string {
	if sourceCount >= highConfidenceMin && avgScore >= highTierScoreThreshold {
		return db.FactCheckTierHigh
	}

	if sourceCount >= 1 && avgScore >= minMatchScore {
		return db.FactCheckTierMedium
	}

	return db.FactCheckTierLow
}

func (s *Scorer) CalculateOverallScore(scores []float32) float32 {
	if len(scores) == 0 {
		return 0
	}

	var sum float32
	for _, score := range scores {
		sum += score
	}

	return sum / float32(len(scores))
}

func (s *Scorer) MarshalMatchedClaims(claims []MatchedClaim) []byte {
	if len(claims) == 0 {
		return nil
	}

	data, err := json.Marshal(claims)
	if err != nil {
		return nil
	}

	return data
}

func tokenize(text string) map[string]bool {
	tokens := make(map[string]bool)
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	for _, word := range words {
		if len(word) >= minTokenLength && !isStopWord(word) {
			tokens[word] = true
		}
	}

	return tokens
}

func jaccardSimilarity(set1, set2 map[string]bool) float64 {
	if len(set1) == 0 || len(set2) == 0 {
		return 0
	}

	intersection := 0

	for token := range set1 {
		if set2[token] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func entityOverlapRatio(entities1, entities2 []Entity) float64 {
	if len(entities1) == 0 || len(entities2) == 0 {
		return 0
	}

	matches := 0
	totalWeight := 0.0

	for _, e1 := range entities1 {
		totalWeight += getEntityWeight(e1.Type)

		if entityMatchExists(e1, entities2) {
			matches++
		}
	}

	if totalWeight == 0 {
		return 0
	}

	return float64(matches) / totalWeight
}

func getEntityWeight(entityType string) float64 {
	if entityType == entityTypePerson || entityType == entityTypeOrg || entityType == entityTypeLoc {
		return entityTypeBoost
	}

	return 1.0
}

func entityMatchExists(entity Entity, candidates []Entity) bool {
	norm1 := normalizeEntity(entity.Text)
	for _, candidate := range candidates {
		if entity.Type != candidate.Type {
			continue
		}

		norm2 := normalizeEntity(candidate.Text)
		if norm1 == norm2 {
			return true
		}

		// Check for aliases
		if isAlias(norm1, norm2) {
			return true
		}

		// Check for partial match (one name is a prefix of another, but not too short)
		if len(norm1) > 4 && len(norm2) > 4 {
			if strings.HasPrefix(norm1, norm2) || strings.HasPrefix(norm2, norm1) {
				return true
			}
		}
	}

	return false
}

func normalizeEntity(text string) string {
	text = strings.ToLower(text)
	text = strings.TrimSpace(text)
	// Remove common suffixes
	text = strings.TrimSuffix(text, " inc")
	text = strings.TrimSuffix(text, " corp")
	text = strings.TrimSuffix(text, " ltd")
	text = strings.TrimSuffix(text, " llc")
	text = strings.TrimSuffix(text, " limited")

	// Basic transliteration for common RU-EN names
	text = transliterate(text)

	// Remove punctuation
	var b strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}

	return b.String()
}

func isAlias(s1, s2 string) bool {
	aliases := map[string][]string{
		"usa":           {"unitedstates", "unitedstatesofamerica", "us"},
		"uk":            {"unitedkingdom", "britain", "greatbritain"},
		"un":            {"unitednations"},
		"eu":            {"europeanunion"},
		"russia":        {"russianfederation"},
		"uae":           {"unitedarabemirates"},
		"apple":         {"appleinc"},
		"microsoft":     {"microsoftcorp"},
		"google":        {"alphabetinc"},
		"meta":          {"facebook"},
		"openai":        {"chatgpt"},
		"donaldtrump":   {"trump"},
		"joebiden":      {"biden"},
		"vladimirputin": {"putin"},
		"kyiv":          {"kiev"},
		"zelenskyy":     {"zelensky"},
		"netanyahu":     {"bibi"},
		"macron":        {"emmanuelmacron"},
		"scholz":        {"olafscholz"},
	}

	for k, vals := range aliases {
		if s1 == k {
			for _, v := range vals {
				if s2 == v {
					return true
				}
			}
		}

		if s2 == k {
			for _, v := range vals {
				if s1 == v {
					return true
				}
			}
		}
	}

	return false
}

func transliterate(text string) string {
	// Simple RU -> EN transliteration for common names
	ruToEn := map[rune]string{
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
		'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
		'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
		'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch",
		'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	}

	var b strings.Builder

	for _, r := range text {
		if en, ok := ruToEn[r]; ok {
			b.WriteString(en)
		} else {
			b.WriteRune(r)
		}
	}

	return b.String()
}

func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"up": true, "about": true, "into": true, "through": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"could": true, "should": true, "may": true, "might": true, "must": true,
		"that": true, "which": true, "who": true, "whom": true, "this": true,
		"these": true, "those": true, "it": true, "its": true, "as": true,
	}

	return stopWords[word]
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

// detectContradiction checks if evidence claims contradict the item claim.
// Contradiction is detected when:
// 1. There's significant entity overlap (claims are about the same thing)
// 2. But negation words or opposing sentiment are present
func detectContradiction(itemText string, claims []ExtractedClaim, itemEntities []Entity) bool {
	if len(claims) == 0 {
		return false
	}

	itemLower := strings.ToLower(itemText)

	for _, claim := range claims {
		// Check entity overlap
		overlap := entityOverlapRatio(itemEntities, claim.Entities)
		if overlap < contradictionThreshold {
			continue // Not enough overlap to be about the same subject
		}

		// Check for negation patterns that might indicate contradiction
		if hasContradictingPatterns(itemLower, strings.ToLower(claim.Text)) {
			return true
		}
	}

	return false
}

// hasContradictingPatterns checks for negation and opposing patterns.
func hasContradictingPatterns(itemText, claimText string) bool {
	// Negation words that might indicate contradiction
	negationWords := []string{
		"not", "no", "never", "none", "nothing", "neither",
		"deny", "denied", "denies", "false", "untrue", "incorrect",
		"wrong", "misleading", "debunked", "refuted", "disproven",
		"contrary", "opposite", "however", "but actually",
	}

	// Check if item has a claim that evidence negates
	for _, neg := range negationWords {
		// Evidence contains negation that item doesn't
		if strings.Contains(claimText, neg) && !strings.Contains(itemText, neg) {
			return true
		}

		// Item contains negation that evidence doesn't (reversed claim)
		if strings.Contains(itemText, neg) && !strings.Contains(claimText, neg) {
			return true
		}
	}

	// Check for opposing number patterns (e.g., "increased" vs "decreased")
	opposingPairs := [][2]string{
		{"increased", "decreased"},
		{"rose", "fell"},
		{"gained", "lost"},
		{"up", "down"},
		{"higher", "lower"},
		{"more", "less"},
		{"growth", "decline"},
		{"success", "failure"},
		{"approved", "rejected"},
		{"confirmed", "denied"},
	}

	for _, pair := range opposingPairs {
		if containsOpposingPair(itemText, claimText, pair[0], pair[1]) {
			return true
		}
	}

	return false
}

// containsOpposingPair checks if one text has word A and the other has word B.
func containsOpposingPair(text1, text2, wordA, wordB string) bool {
	text1HasA := strings.Contains(text1, wordA)
	text1HasB := strings.Contains(text1, wordB)
	text2HasA := strings.Contains(text2, wordA)
	text2HasB := strings.Contains(text2, wordB)

	// One text has A but not B, other has B but not A
	return (text1HasA && !text1HasB && text2HasB && !text2HasA) ||
		(text1HasB && !text1HasA && text2HasA && !text2HasB)
}
