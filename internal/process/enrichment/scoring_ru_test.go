package enrichment

import (
	"testing"
)

const expectedMatchedClaims = "expected matched claims"

func TestScorer_Score_RussianVsEnglish(t *testing.T) {
	s := NewScorer()

	itemSummary := "Владимир Путин посетил Москву для встречи с представителями власти."
	// Entities expected: Vladimir Putin, Moscow (transliterated)

	evidence := &ExtractedEvidence{
		Claims: []ExtractedClaim{
			{
				Text: "Vladimir Putin arrived in Moscow for official talks.",
				Entities: []Entity{
					{Text: "Vladimir Putin", Type: entityTypePerson},
					{Text: "Moscow", Type: entityTypeLoc},
				},
			},
		},
	}

	result := s.Score(itemSummary, evidence)

	// Jaccard will be 0 because of different languages/alphabets
	// Entity overlap should be 1.0 (both match after transliteration and alias)
	// Score = 0.6 * 0 + 0.4 * 1.0 = 0.4

	if result.AgreementScore < 0.35 {
		t.Errorf("expected score >= 0.35 for RU-EN match, got %f", result.AgreementScore)
	}

	if len(result.MatchedClaims) == 0 {
		t.Error(expectedMatchedClaims)
	}
}

func TestScorer_Score_RussianStemming(t *testing.T) {
	s := NewScorer()

	itemSummary := "Население Земли растет быстрыми темпами."
	evidence := &ExtractedEvidence{
		Claims: []ExtractedClaim{
			{
				Text:     "Население Земля растет быстрыми темпами.",
				Entities: []Entity{},
			},
		},
	}

	result := s.Score(itemSummary, evidence)
	if result.AgreementScore < 0.2 {
		t.Errorf("expected score >= 0.2 for RU stemming match, got %f", result.AgreementScore)
	}

	if len(result.MatchedClaims) == 0 {
		t.Error(expectedMatchedClaims)
	}
}
