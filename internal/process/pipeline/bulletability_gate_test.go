package pipeline

import (
	"math"
	"strings"
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

const (
	bulletabilityBandInconclusive = "inconclusive"
)

func TestComputeBulletabilityScore_Fixtures(t *testing.T) {
	t.Run("list with markers is bulletable", func(t *testing.T) {
		score := computeBulletabilityScore("- A\n- B\n- C")
		if math.Abs(score-0.9) > 1e-6 {
			t.Fatalf("expected score 0.9, got %v", score)
		}
	})

	t.Run("narrative paragraph is not bulletable", func(t *testing.T) {
		score := computeBulletabilityScore("This is one continuous narrative paragraph without list markers or section separators.")
		if score != 0 {
			t.Fatalf("expected score 0, got %v", score)
		}
	})

	t.Run("two markers with continuation is inconclusive", func(t *testing.T) {
		score := computeBulletabilityScore("— Point A\n— Point B\nContinuation text...")
		if math.Abs(score-0.55) > 1e-6 {
			t.Fatalf("expected score 0.55, got %v", score)
		}
	})
}

func TestComputeBulletabilityScore_DecisionBands(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		wantBandName string
	}{
		{
			name:         "high threshold",
			text:         "- One\n- Two\n- Three",
			wantBandName: bulletabilityResultBulletable,
		},
		{
			name:         "low threshold",
			text:         "Single narrative sentence with no list structure.",
			wantBandName: bulletabilityResultNotBulletable,
		},
		{
			name:         "inconclusive band",
			text:         "— One\n— Two\nContinuation",
			wantBandName: bulletabilityBandInconclusive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeBulletabilityScore(tt.text)
			switch {
			case score >= bulletabilityHighThreshold && tt.wantBandName == bulletabilityResultBulletable:
			case score <= bulletabilityLowThreshold && tt.wantBandName == bulletabilityResultNotBulletable:
			case score > bulletabilityLowThreshold && score < bulletabilityHighThreshold && tt.wantBandName == bulletabilityBandInconclusive:
			default:
				t.Fatalf("score %v did not fall into expected band %q", score, tt.wantBandName)
			}
		})
	}
}

func TestBuildBulletabilityClassifierInput_TruncatesDeterministically(t *testing.T) {
	longText := strings.Repeat("x", bulletabilityClassifierMaxRunes+120)

	input := buildBulletabilityClassifierInput(llm.MessageInput{}, "", "")
	if input != "" {
		t.Fatalf("expected empty input for empty candidate, got %q", input)
	}

	candidate := llm.MessageInput{RawMessage: domain.RawMessage{Text: longText}}

	out := buildBulletabilityClassifierInput(candidate, "", "")
	if len([]rune(out)) != bulletabilityClassifierMaxRunes {
		t.Fatalf("expected %d runes, got %d", bulletabilityClassifierMaxRunes, len([]rune(out)))
	}

	out2 := buildBulletabilityClassifierInput(candidate, "", "")
	if out != out2 {
		t.Fatal("expected deterministic truncation output")
	}
}

func TestBuildBulletabilityScoreText_NoDuplicatePreview(t *testing.T) {
	text := "same content"

	out := buildBulletabilityScoreText(text, text, "")
	if strings.Count(out, text) != 1 {
		t.Fatalf("expected text to appear once, got %q", out)
	}
}
