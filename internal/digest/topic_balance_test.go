package digest

import (
	"math"
	"testing"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

func TestApplyFreshnessDecayFloor(t *testing.T) {
	now := time.Now()
	score := applyFreshnessDecay(1.0, now.Add(-36*time.Hour), 36, 0.4)
	if math.Abs(float64(score-0.4)) > 0.01 {
		t.Fatalf("applyFreshnessDecay floor = %v, want ~0.4", score)
	}
}

func TestApplyTopicBalanceCapAndMin(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "B"},
		{Topic: "B"},
		{Topic: "C"},
		{Topic: "C"},
	}

	result := applyTopicBalance(items, 5, 0.4, 3)
	if len(result.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(result.Items))
	}

	counts := make(map[string]int)
	for _, item := range result.Items {
		key, _ := topicKey(item.Topic)
		counts[key]++
	}

	if counts["a"] > 2 || counts["b"] > 2 || counts["c"] > 2 {
		t.Fatalf("topic cap violated: %v", counts)
	}
	if result.TopicsSelected != 3 {
		t.Fatalf("expected 3 topics selected, got %d", result.TopicsSelected)
	}
}

func TestApplyTopicBalanceRelaxesCap(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "B"},
	}

	result := applyTopicBalance(items, 4, 0.25, 0)
	if len(result.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result.Items))
	}
	if !result.Relaxed {
		t.Fatalf("expected relaxed cap when topics are insufficient")
	}
}
