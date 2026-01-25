package dedup

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	testSimilarityThreshold = 0.9
	testErrIsDuplicate      = "IsDuplicate() error = %v, wantErr %v"
	testErrIsDup            = "IsDuplicate() isDup = %v, want %v"
	testErrDupID            = "IsDuplicate() dupID = %q, want %q"
)

var (
	errDatabase     = errors.New("database error")
	errDBConnection = errors.New("db connection failed")
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "different lengths",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0},
			expected: 0.0,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
		{
			name:     "zero vectors",
			a:        []float32{0, 0, 0},
			b:        []float32{1, 1, 1},
			expected: 0.0,
		},
		{
			name:     "typical similarity",
			a:        []float32{1, 1, 0},
			b:        []float32{1, 0, 0},
			expected: float32(1.0 / math.Sqrt(2.0)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(got-tt.expected)) > 1e-6 {
				t.Errorf("CosineSimilarity() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// mockRepository implements Repository for testing.
type mockRepository struct {
	strictDuplicateExists bool
	strictDuplicateErr    error
	similarItemID         string
	similarItemErr        error
}

func (m *mockRepository) CheckStrictDuplicate(_ context.Context, _, _ string) (bool, error) {
	return m.strictDuplicateExists, m.strictDuplicateErr
}

func (m *mockRepository) FindSimilarItem(_ context.Context, _ []float32, _ float32, _ time.Time) (string, error) {
	return m.similarItemID, m.similarItemErr
}

func TestNewSemantic(t *testing.T) {
	repo := &mockRepository{}
	d := NewSemantic(repo, testSimilarityThreshold, 0)

	if d == nil {
		t.Fatal("NewSemantic() returned nil")
	}
}

func TestNewStrict(t *testing.T) {
	repo := &mockRepository{}
	d := NewStrict(repo)

	if d == nil {
		t.Fatal("NewStrict() returned nil")
	}
}

func TestSemanticDeduplicator_IsDuplicate(t *testing.T) {
	tests := []struct {
		name          string
		embedding     []float32
		similarItemID string
		similarErr    error
		wantDup       bool
		wantDupID     string
		wantErr       bool
	}{
		{
			name:          "empty embedding returns not duplicate",
			embedding:     []float32{},
			similarItemID: "",
			wantDup:       false,
			wantDupID:     "",
			wantErr:       false,
		},
		{
			name:          "no similar item found",
			embedding:     []float32{1.0, 0.0, 0.0},
			similarItemID: "",
			wantDup:       false,
			wantDupID:     "",
			wantErr:       false,
		},
		{
			name:          "similar item found",
			embedding:     []float32{1.0, 0.0, 0.0},
			similarItemID: "existing-item-123",
			wantDup:       true,
			wantDupID:     "existing-item-123",
			wantErr:       false,
		},
		{
			name:       "repository error",
			embedding:  []float32{1.0, 0.0, 0.0},
			similarErr: errDatabase,
			wantDup:    false,
			wantDupID:  "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepository{
				similarItemID:  tt.similarItemID,
				similarItemErr: tt.similarErr,
			}
			d := NewSemantic(repo, testSimilarityThreshold, 0)

			isDup, dupID, err := d.IsDuplicate(context.Background(), db.RawMessage{}, tt.embedding)

			if (err != nil) != tt.wantErr {
				t.Errorf(testErrIsDuplicate, err, tt.wantErr)
				return
			}

			if isDup != tt.wantDup {
				t.Errorf(testErrIsDup, isDup, tt.wantDup)
			}

			if dupID != tt.wantDupID {
				t.Errorf(testErrDupID, dupID, tt.wantDupID)
			}
		})
	}
}

func TestStrictDeduplicator_IsDuplicate(t *testing.T) {
	tests := []struct {
		name      string
		msg       db.RawMessage
		dupExists bool
		dupErr    error
		wantDup   bool
		wantDupID string
		wantErr   bool
	}{
		{
			name: "no duplicate exists",
			msg: db.RawMessage{
				ID:            "msg-1",
				CanonicalHash: "hash-abc",
			},
			dupExists: false,
			wantDup:   false,
			wantDupID: "",
			wantErr:   false,
		},
		{
			name: "duplicate exists",
			msg: db.RawMessage{
				ID:            "msg-2",
				CanonicalHash: "hash-abc",
			},
			dupExists: true,
			wantDup:   true,
			wantDupID: "strict_duplicate",
			wantErr:   false,
		},
		{
			name: "repository error",
			msg: db.RawMessage{
				ID:            "msg-3",
				CanonicalHash: "hash-def",
			},
			dupErr:  errDBConnection,
			wantDup: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepository{
				strictDuplicateExists: tt.dupExists,
				strictDuplicateErr:    tt.dupErr,
			}
			d := NewStrict(repo)

			isDup, dupID, err := d.IsDuplicate(context.Background(), tt.msg, nil)

			if (err != nil) != tt.wantErr {
				t.Errorf(testErrIsDuplicate, err, tt.wantErr)
				return
			}

			if isDup != tt.wantDup {
				t.Errorf(testErrIsDup, isDup, tt.wantDup)
			}

			if dupID != tt.wantDupID {
				t.Errorf(testErrDupID, dupID, tt.wantDupID)
			}
		})
	}
}
