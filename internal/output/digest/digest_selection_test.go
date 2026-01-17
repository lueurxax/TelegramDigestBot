package digest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Test constants for digest selection tests
const (
	testNameSortsByImportanceDesc = "items should be sorted by importance descending"
	testNameEmptyItemsReturns     = "empty items returns empty"
	testNameEmptyInputReturns     = "empty input returns empty"
	testErrExpected2Items         = "expected 2 items, got %d"
	testErrExpected1Item          = "expected 1 item, got %d"
	testChannelName               = "testchan"
	testSummaryText               = "Test summary"
)

func TestApplySmartSelection(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{}}

	t.Run("applies freshness decay", func(t *testing.T) {
		now := time.Now()
		items := []db.Item{
			{ID: "1", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "ch1"},
			{ID: "2", ImportanceScore: testHalfValue, TGDate: now.Add(-48 * time.Hour), SourceChannel: "ch2"},
		}
		settings := digestSettings{
			freshnessDecayHours: 24,
			freshnessFloor:      0.3,
		}

		result := s.applySmartSelection(items, settings)

		// First item (newer) should have higher score than second (older)
		if result[0].ImportanceScore <= result[1].ImportanceScore {
			t.Errorf("newer item should have higher score after decay")
		}
	})

	t.Run("applies source diversity bonus", func(t *testing.T) {
		now := time.Now()
		items := []db.Item{
			{ID: "1", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "unique"},
			{ID: "2", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "common"},
			{ID: "3", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "common"},
		}
		settings := digestSettings{
			freshnessDecayHours: 0, // Disable decay
		}

		result := s.applySmartSelection(items, settings)

		// Unique source should get bonus
		if result[0].SourceChannel != "unique" {
			t.Errorf("unique source should be first after diversity bonus")
		}
	})

	t.Run("sorts by importance descending", func(t *testing.T) {
		now := time.Now()
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.3, TGDate: now, SourceChannel: "ch1"},
			{ID: "2", ImportanceScore: 0.9, TGDate: now, SourceChannel: "ch2"},
			{ID: "3", ImportanceScore: 0.6, TGDate: now, SourceChannel: "ch3"},
		}
		settings := digestSettings{freshnessDecayHours: 0}

		result := s.applySmartSelection(items, settings)

		// Should be sorted by importance
		if result[0].ImportanceScore < result[1].ImportanceScore {
			t.Error(testNameSortsByImportanceDesc)
		}

		if result[1].ImportanceScore < result[2].ImportanceScore {
			t.Error(testNameSortsByImportanceDesc)
		}
	})

	t.Run("ties broken by relevance score", func(t *testing.T) {
		now := time.Now()
		items := []db.Item{
			{ID: "1", ImportanceScore: testHalfValue, RelevanceScore: 0.3, TGDate: now, SourceChannel: "ch1"},
			{ID: "2", ImportanceScore: testHalfValue, RelevanceScore: 0.9, TGDate: now, SourceChannel: "ch2"},
		}
		settings := digestSettings{freshnessDecayHours: 0}

		result := s.applySmartSelection(items, settings)

		// Higher relevance should come first when importance is equal
		if result[0].RelevanceScore < result[1].RelevanceScore {
			t.Errorf("higher relevance should come first on tie")
		}
	})

	t.Run(testNameEmptyItemsReturns, func(t *testing.T) {
		settings := digestSettings{}

		result := s.applySmartSelection(nil, settings)
		if len(result) != 0 {
			t.Errorf(testErrExpected0Items, len(result))
		}
	})
}

func TestDeduplicateItems(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{SimilarityThreshold: 0.9}}
	logger := zerolog.Nop()

	t.Run("keeps items without embeddings", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Embedding: nil},
			{ID: "2", Embedding: nil},
		}

		result := s.deduplicateItems(items, &logger)

		if len(result) != 2 {
			t.Errorf(testErrExpected2Items, len(result))
		}
	})

	t.Run("removes duplicates with similar embeddings", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Embedding: []float32{1, 0, 0, 0}},
			{ID: "2", Embedding: []float32{1, 0, 0, 0}}, // Identical
		}

		result := s.deduplicateItems(items, &logger)

		if len(result) != 1 {
			t.Errorf("expected 1 item (duplicate removed), got %d", len(result))
		}

		if result[0].ID != "1" {
			t.Errorf("first item should be kept, got %s", result[0].ID)
		}
	})

	t.Run("keeps items with different embeddings", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Embedding: []float32{1, 0, 0, 0}},
			{ID: "2", Embedding: []float32{0, 1, 0, 0}}, // Orthogonal
		}

		result := s.deduplicateItems(items, &logger)

		if len(result) != 2 {
			t.Errorf("expected 2 items (different embeddings), got %d", len(result))
		}
	})

	t.Run("mixed items with and without embeddings", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Embedding: []float32{1, 0, 0, 0}},
			{ID: "2", Embedding: nil},
			{ID: "3", Embedding: []float32{1, 0, 0, 0}}, // Duplicate of 1
		}

		result := s.deduplicateItems(items, &logger)

		if len(result) != 2 {
			t.Errorf(testErrExpected2Items, len(result))
		}
	})

	t.Run(testNameEmptyInputReturns, func(t *testing.T) {
		result := s.deduplicateItems(nil, &logger)
		if len(result) != 0 {
			t.Errorf(testErrExpected0Items, len(result))
		}
	})
}

func TestApplyTopicBalanceAndLimit(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{DigestTopN: 5}}
	logger := zerolog.Nop()

	t.Run("limits to topN when topic balance disabled", func(t *testing.T) {
		items := make([]db.Item, 10)
		for i := range items {
			items[i] = db.Item{ID: string(rune('0' + i)), Topic: "Topic"}
		}

		settings := digestSettings{
			topicsEnabled:     false,
			topicDiversityCap: 0,
		}

		result := s.applyTopicBalanceAndLimit(items, settings, &logger)

		if len(result) != 5 {
			t.Errorf("expected 5 items (topN limit), got %d", len(result))
		}
	})

	t.Run("applies topic balance when enabled", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Topic: "A"},
			{ID: "2", Topic: "A"},
			{ID: "3", Topic: "A"},
			{ID: "4", Topic: "B"},
			{ID: "5", Topic: "B"},
			{ID: "6", Topic: "C"},
		}

		settings := digestSettings{
			topicsEnabled:     true,
			topicDiversityCap: 0.5,
			minTopicCount:     2,
		}

		result := s.applyTopicBalanceAndLimit(items, settings, &logger)

		if len(result) != 5 {
			t.Errorf("expected 5 items, got %d", len(result))
		}
	})

	t.Run(testNameEmptyItemsReturns, func(t *testing.T) {
		settings := digestSettings{topicsEnabled: true}

		result := s.applyTopicBalanceAndLimit(nil, settings, &logger)
		if len(result) != 0 {
			t.Errorf(testErrExpected0Items, len(result))
		}
	})

	t.Run("items less than topN returns all", func(t *testing.T) {
		items := []db.Item{{ID: "1"}, {ID: "2"}}
		settings := digestSettings{topicsEnabled: false}

		result := s.applyTopicBalanceAndLimit(items, settings, &logger)

		if len(result) != 2 {
			t.Errorf(testErrExpected2Items, len(result))
		}
	})

	t.Run("invalid cap fraction bypasses balance", func(t *testing.T) {
		items := make([]db.Item, 10)
		for i := range items {
			items[i] = db.Item{ID: string(rune('0' + i)), Topic: "Topic"}
		}

		settings := digestSettings{
			topicsEnabled:     true,
			topicDiversityCap: 1.5, // Invalid (>= 1)
		}

		result := s.applyTopicBalanceAndLimit(items, settings, &logger)

		if len(result) != 5 {
			t.Errorf("expected 5 items (fallback to topN), got %d", len(result))
		}
	})
}

func TestApplySmartSelectionChannelCounting(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{}}
	now := time.Now()

	t.Run("multiple unique channels all get bonus", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "ch1"},
			{ID: "2", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "ch2"},
			{ID: "3", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "ch3"},
		}
		settings := digestSettings{freshnessDecayHours: 0}

		result := s.applySmartSelection(items, settings)

		// All should have bonus (each channel appears once)
		for _, item := range result {
			if item.ImportanceScore != testHalfValue+SourceDiversityBonus {
				t.Errorf("item %s should have diversity bonus", item.ID)
			}
		}
	})

	t.Run("duplicate channels do not get bonus", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "ch1"},
			{ID: "2", ImportanceScore: testHalfValue, TGDate: now, SourceChannel: "ch1"},
		}
		settings := digestSettings{freshnessDecayHours: 0}

		result := s.applySmartSelection(items, settings)

		// Neither should have bonus (channel appears twice)
		for _, item := range result {
			if item.ImportanceScore != testHalfValue {
				t.Errorf("item %s should not have diversity bonus", item.ID)
			}
		}
	})
}

func TestDeduplicateItemsSimilarityThresholds(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("high threshold keeps different items", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{SimilarityThreshold: 0.99}}
		// Vectors with lower similarity (around 0.7)
		items := []db.Item{
			{ID: "1", Embedding: []float32{1, 0, 0, 0}},
			{ID: "2", Embedding: []float32{0.7, 0.7, 0, 0}}, // Different direction
		}

		result := s.deduplicateItems(items, &logger)
		if len(result) != 2 {
			t.Errorf("high threshold should keep both items, got %d", len(result))
		}
	})

	t.Run("low threshold removes similar items", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{SimilarityThreshold: 0.5}}
		items := []db.Item{
			{ID: "1", Embedding: []float32{1, 0, 0, 0}},
			{ID: "2", Embedding: []float32{0.9, 0.1, 0, 0}}, // Similar
		}

		result := s.deduplicateItems(items, &logger)
		if len(result) != 1 {
			t.Errorf("low threshold should remove similar item, got %d", len(result))
		}
	})
}

func TestApplySmartSelectionWithDecay(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{}}
	now := time.Now()

	t.Run("very old items decay significantly", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 1.0, TGDate: now.Add(-72 * time.Hour), SourceChannel: "ch1"},
		}
		settings := digestSettings{
			freshnessDecayHours: 24,
			freshnessFloor:      0.3,
		}

		result := s.applySmartSelection(items, settings)

		// After 3 time constants, decay should be significant
		// But floored at 0.3, so final should be around 0.3
		if result[0].ImportanceScore > 0.5 {
			t.Errorf("very old item should have decayed score, got %v", result[0].ImportanceScore)
		}
	})

	t.Run("fresh items barely decay", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 1.0, TGDate: now.Add(-1 * time.Hour), SourceChannel: "ch1"},
		}
		settings := digestSettings{
			freshnessDecayHours: 24,
			freshnessFloor:      0.3,
		}

		result := s.applySmartSelection(items, settings)

		// After 1 hour with 24-hour decay, should be ~0.96
		if result[0].ImportanceScore < 0.9 {
			t.Errorf("fresh item should barely decay, got %v", result[0].ImportanceScore)
		}
	})
}

func TestDeduplicateItemsPreservesOrder(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{SimilarityThreshold: 0.9}}
	logger := zerolog.Nop()

	items := []db.Item{
		{ID: "1", Embedding: []float32{1, 0, 0, 0}},
		{ID: "2", Embedding: []float32{0, 1, 0, 0}},
		{ID: "3", Embedding: []float32{0, 0, 1, 0}},
		{ID: "4", Embedding: []float32{0, 0, 0, 1}},
	}

	result := s.deduplicateItems(items, &logger)

	for i, item := range result {
		expected := string(rune('1' + i))
		if item.ID != expected {
			t.Errorf("item %d should be %s, got %s", i, expected, item.ID)
		}
	}
}

func TestApplyTopicBalanceAndLimitZeroTopN(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{DigestTopN: 0}}
	logger := zerolog.Nop()

	items := []db.Item{{ID: "1"}, {ID: "2"}}
	settings := digestSettings{
		topicsEnabled:     true,
		topicDiversityCap: 0.5,
		minTopicCount:     0,
	}

	result := s.applyTopicBalanceAndLimit(items, settings, &logger)

	// With topN = 0, applyTopicBalance returns nil items
	// This is expected behavior based on the function logic
	if len(result) > 0 {
		t.Errorf("expected empty result with topN=0, got %d", len(result))
	}
}

func TestApplySmartSelectionPreservesItemData(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{}}
	now := time.Now()

	items := []db.Item{
		{
			ID:              "1",
			ImportanceScore: testHalfValue,
			RelevanceScore:  0.7,
			TGDate:          now,
			SourceChannel:   testChannelName,
			Summary:         testSummaryText,
			Topic:           testTopicTechnology,
		},
	}
	settings := digestSettings{freshnessDecayHours: 0}

	result := s.applySmartSelection(items, settings)

	if len(result) != 1 {
		t.Fatalf(testErrExpected1Item, len(result))
	}

	// Verify non-score fields are preserved
	if result[0].ID != "1" {
		t.Errorf("ID should be preserved, got %s", result[0].ID)
	}

	if result[0].SourceChannel != testChannelName {
		t.Errorf("SourceChannel should be preserved, got %s", result[0].SourceChannel)
	}

	if result[0].Summary != testSummaryText {
		t.Errorf("Summary should be preserved, got %s", result[0].Summary)
	}

	if result[0].Topic != testTopicTechnology {
		t.Errorf("Topic should be preserved, got %s", result[0].Topic)
	}
}
