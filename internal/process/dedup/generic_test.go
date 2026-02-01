package dedup

import (
	"testing"
	"time"
)

// mockScorable implements domain.Scorable for testing.
type mockScorable struct {
	id              string
	content         string
	importanceScore float32
	relevanceScore  float32
	topic           string
	embedding       []float32
	timestamp       time.Time
	sourceID        string
}

func (m *mockScorable) GetID() string                    { return m.id }
func (m *mockScorable) GetContent() string               { return m.content }
func (m *mockScorable) GetImportanceScore() float32      { return m.importanceScore }
func (m *mockScorable) SetImportanceScore(score float32) { m.importanceScore = score }
func (m *mockScorable) GetRelevanceScore() float32       { return m.relevanceScore }
func (m *mockScorable) SetRelevanceScore(score float32)  { m.relevanceScore = score }
func (m *mockScorable) GetTopic() string                 { return m.topic }
func (m *mockScorable) SetTopic(topic string)            { m.topic = topic }
func (m *mockScorable) GetEmbedding() []float32          { return m.embedding }
func (m *mockScorable) SetEmbedding(embedding []float32) { m.embedding = embedding }
func (m *mockScorable) GetTimestamp() time.Time          { return m.timestamp }
func (m *mockScorable) GetSourceID() string              { return m.sourceID }

func TestDeduplicateScorables(t *testing.T) {
	tests := []struct {
		name      string
		items     []*mockScorable
		threshold float32
		wantLen   int
		wantIDs   []string
	}{
		{
			name:      "empty input",
			items:     []*mockScorable{},
			threshold: 0.9,
			wantLen:   0,
			wantIDs:   nil,
		},
		{
			name: "single item",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   1,
			wantIDs:   []string{"1"},
		},
		{
			name: "no duplicates - orthogonal embeddings",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{0, 1, 0}},
				{id: "3", embedding: []float32{0, 0, 1}},
			},
			threshold: 0.9,
			wantLen:   3,
			wantIDs:   []string{"1", "2", "3"},
		},
		{
			name: "duplicate removed - identical embeddings",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   1,
			wantIDs:   []string{"1"},
		},
		{
			name: "duplicate removed - similar embeddings above threshold",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{0.95, 0.05, 0}},
			},
			threshold: 0.9,
			wantLen:   1,
			wantIDs:   []string{"1"},
		},
		{
			name: "kept - similar but below threshold",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{0.7, 0.7, 0}},
			},
			threshold: 0.9,
			wantLen:   2,
			wantIDs:   []string{"1", "2"},
		},
		{
			name: "item without embedding kept",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: nil},
				{id: "3", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   2,
			wantIDs:   []string{"1", "2"},
		},
		{
			name: "multiple duplicates",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{1, 0, 0}},
				{id: "3", embedding: []float32{0, 1, 0}},
				{id: "4", embedding: []float32{0, 1, 0}},
			},
			threshold: 0.9,
			wantLen:   2,
			wantIDs:   []string{"1", "3"},
		},
		{
			name: "skip comparison when kept item has no embedding",
			items: []*mockScorable{
				{id: "1", embedding: nil},
				{id: "2", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   2,
			wantIDs:   []string{"1", "2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeduplicateScorables(tt.items, tt.threshold, nil)
			if len(got) != tt.wantLen {
				t.Errorf("DeduplicateScorables() len = %d, want %d", len(got), tt.wantLen)
			}

			for i, item := range got {
				if i < len(tt.wantIDs) && item.GetID() != tt.wantIDs[i] {
					t.Errorf("DeduplicateScorables()[%d].GetID() = %s, want %s", i, item.GetID(), tt.wantIDs[i])
				}
			}
		})
	}
}

func TestDeduplicateScorablesFull(t *testing.T) {
	tests := []struct {
		name             string
		items            []*mockScorable
		threshold        float32
		wantLen          int
		wantDroppedCount int
		wantDupMapLen    int
	}{
		{
			name:             "empty input",
			items:            []*mockScorable{},
			threshold:        0.9,
			wantLen:          0,
			wantDroppedCount: 0,
			wantDupMapLen:    0,
		},
		{
			name: "no duplicates",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{0, 1, 0}},
			},
			threshold:        0.9,
			wantLen:          2,
			wantDroppedCount: 0,
			wantDupMapLen:    0,
		},
		{
			name: "one duplicate",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{1, 0, 0}},
			},
			threshold:        0.9,
			wantLen:          1,
			wantDroppedCount: 1,
			wantDupMapLen:    1,
		},
		{
			name: "multiple duplicates tracked",
			items: []*mockScorable{
				{id: "1", embedding: []float32{1, 0, 0}},
				{id: "2", embedding: []float32{1, 0, 0}},
				{id: "3", embedding: []float32{1, 0, 0}},
			},
			threshold:        0.9,
			wantLen:          1,
			wantDroppedCount: 2,
			wantDupMapLen:    2,
		},
		{
			name: "item without embedding kept",
			items: []*mockScorable{
				{id: "1", embedding: nil},
				{id: "2", embedding: []float32{1, 0, 0}},
			},
			threshold:        0.9,
			wantLen:          2,
			wantDroppedCount: 0,
			wantDupMapLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeduplicateScorablesFull(tt.items, tt.threshold, nil)
			if len(result.Items) != tt.wantLen {
				t.Errorf("DeduplicateScorablesFull() Items len = %d, want %d", len(result.Items), tt.wantLen)
			}

			if result.DroppedCount != tt.wantDroppedCount {
				t.Errorf("DeduplicateScorablesFull() DroppedCount = %d, want %d", result.DroppedCount, tt.wantDroppedCount)
			}

			if len(result.DuplicateMap) != tt.wantDupMapLen {
				t.Errorf("DeduplicateScorablesFull() DuplicateMap len = %d, want %d", len(result.DuplicateMap), tt.wantDupMapLen)
			}
		})
	}
}

func TestDeduplicateScorablesFull_DuplicateMap(t *testing.T) {
	items := []*mockScorable{
		{id: "original", embedding: []float32{1, 0, 0}},
		{id: "dup1", embedding: []float32{1, 0, 0}},
		{id: "dup2", embedding: []float32{1, 0, 0}},
	}

	result := DeduplicateScorablesFull(items, 0.9, nil)

	const originalID = "original"

	if result.DuplicateMap["dup1"] != originalID {
		t.Errorf("DuplicateMap[dup1] = %s, want %s", result.DuplicateMap["dup1"], originalID)
	}

	if result.DuplicateMap["dup2"] != originalID {
		t.Errorf("DuplicateMap[dup2] = %s, want %s", result.DuplicateMap["dup2"], originalID)
	}
}

func TestFindDuplicates(t *testing.T) {
	tests := []struct {
		name       string
		candidates []*mockScorable
		reference  []*mockScorable
		threshold  float32
		wantLen    int
		wantIDs    []string
	}{
		{
			name:       "empty candidates",
			candidates: []*mockScorable{},
			reference: []*mockScorable{
				{id: "ref1", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   0,
		},
		{
			name: "empty reference",
			candidates: []*mockScorable{
				{id: "cand1", embedding: []float32{1, 0, 0}},
			},
			reference: []*mockScorable{},
			threshold: 0.9,
			wantLen:   0,
		},
		{
			name: "no duplicates",
			candidates: []*mockScorable{
				{id: "cand1", embedding: []float32{1, 0, 0}},
			},
			reference: []*mockScorable{
				{id: "ref1", embedding: []float32{0, 1, 0}},
			},
			threshold: 0.9,
			wantLen:   0,
		},
		{
			name: "duplicate found",
			candidates: []*mockScorable{
				{id: "cand1", embedding: []float32{1, 0, 0}},
			},
			reference: []*mockScorable{
				{id: "ref1", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   1,
			wantIDs:   []string{"cand1"},
		},
		{
			name: "candidate without embedding skipped",
			candidates: []*mockScorable{
				{id: "cand1", embedding: nil},
			},
			reference: []*mockScorable{
				{id: "ref1", embedding: []float32{1, 0, 0}},
			},
			threshold: 0.9,
			wantLen:   0,
		},
		{
			name: "reference without embedding skipped",
			candidates: []*mockScorable{
				{id: "cand1", embedding: []float32{1, 0, 0}},
			},
			reference: []*mockScorable{
				{id: "ref1", embedding: nil},
			},
			threshold: 0.9,
			wantLen:   0,
		},
		{
			name: "multiple duplicates",
			candidates: []*mockScorable{
				{id: "cand1", embedding: []float32{1, 0, 0}},
				{id: "cand2", embedding: []float32{0, 1, 0}},
				{id: "cand3", embedding: []float32{0, 0, 1}},
			},
			reference: []*mockScorable{
				{id: "ref1", embedding: []float32{1, 0, 0}},
				{id: "ref2", embedding: []float32{0, 0, 1}},
			},
			threshold: 0.9,
			wantLen:   2,
			wantIDs:   []string{"cand1", "cand3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindDuplicates(tt.candidates, tt.reference, tt.threshold)
			if len(got) != tt.wantLen {
				t.Errorf("FindDuplicates() len = %d, want %d", len(got), tt.wantLen)
			}

			for i, item := range got {
				if i < len(tt.wantIDs) && item.GetID() != tt.wantIDs[i] {
					t.Errorf("FindDuplicates()[%d].GetID() = %s, want %s", i, item.GetID(), tt.wantIDs[i])
				}
			}
		})
	}
}
