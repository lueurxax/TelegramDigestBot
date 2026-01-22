package enrichment

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

const testContentForLLM = "This is a long enough content to pass the minimum length check for LLM processing."

// Test error sentinels.
var (
	errPermanent          = errors.New("some permanent error")
	errWrappedDeadline    = errors.New("llm completion: context deadline exceeded")
	errIOTimeout          = errors.New("read tcp: i/o timeout")
	errConnectionTimeout  = errors.New("dial tcp: connection timed out")
	errTimeoutConfig      = errors.New("feature timeout configuration invalid")
	errSomethingWentWrong = errors.New("something went wrong")
)

// retryMockLLMClient tracks call count and returns configurable errors.
type retryMockLLMClient struct {
	llm.Client
	callCount  atomic.Int32
	errorUntil int32 // Return error until this many calls
	err        error
	response   string
}

func (m *retryMockLLMClient) CompleteText(_ context.Context, _, _ string) (string, error) {
	count := m.callCount.Add(1)
	if count <= m.errorUntil {
		return "", m.err
	}

	return m.response, nil
}

func TestExtractor_RetryOnDeadlineExceeded(t *testing.T) {
	logger := zerolog.Nop()
	m := &retryMockLLMClient{
		errorUntil: 2, // Fail first 2 attempts
		err:        context.DeadlineExceeded,
		response:   `[{"text": "Claim 1", "entities": []}]`,
	}

	e := NewExtractor(&logger)
	e.SetLLMClient(m, testModel)
	e.SetLLMTimeout(100 * time.Millisecond) // Short timeout for test

	claims, err := e.extractClaimsWithLLM(context.Background(), testContentForLLM)

	require.NoError(t, err)
	assert.Len(t, claims, 1)
	assert.Equal(t, int32(3), m.callCount.Load(), "expected 3 calls (2 failures + 1 success)")
}

func TestExtractor_NoRetryOnContextCanceled(t *testing.T) {
	logger := zerolog.Nop()
	m := &retryMockLLMClient{
		errorUntil: 10, // Always fail
		err:        context.Canceled,
		response:   `[{"text": "Claim 1", "entities": []}]`,
	}

	e := NewExtractor(&logger)
	e.SetLLMClient(m, testModel)
	e.SetLLMTimeout(100 * time.Millisecond)

	_, err := e.extractClaimsWithLLM(context.Background(), testContentForLLM)

	require.Error(t, err)
	assert.Equal(t, int32(1), m.callCount.Load(), "should not retry on context.Canceled")
}

func TestExtractor_MaxRetriesExceeded(t *testing.T) {
	logger := zerolog.Nop()
	m := &retryMockLLMClient{
		errorUntil: 10, // Always fail
		err:        context.DeadlineExceeded,
		response:   `[{"text": "Claim 1", "entities": []}]`,
	}

	e := NewExtractor(&logger)
	e.SetLLMClient(m, testModel)
	e.SetLLMTimeout(100 * time.Millisecond)

	_, err := e.extractClaimsWithLLM(context.Background(), testContentForLLM)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
	// llmMaxRetries=2 means 3 total attempts (initial + 2 retries)
	assert.Equal(t, int32(llmMaxRetries+1), m.callCount.Load())
}

func TestExtractor_NoRetryOnNonRetryableError(t *testing.T) {
	logger := zerolog.Nop()
	m := &retryMockLLMClient{
		errorUntil: 10,
		err:        errPermanent,
		response:   `[{"text": "Claim 1", "entities": []}]`,
	}

	e := NewExtractor(&logger)
	e.SetLLMClient(m, testModel)
	e.SetLLMTimeout(100 * time.Millisecond)

	_, err := e.extractClaimsWithLLM(context.Background(), testContentForLLM)

	require.Error(t, err)
	assert.Equal(t, int32(1), m.callCount.Load(), "should not retry on non-retryable error")
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "context.DeadlineExceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "context.Canceled",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "wrapped deadline exceeded",
			err:      errWrappedDeadline,
			expected: true,
		},
		{
			name:     "i/o timeout",
			err:      errIOTimeout,
			expected: true,
		},
		{
			name:     "connection timed out",
			err:      errConnectionTimeout,
			expected: true,
		},
		{
			name:     "generic timeout word should not match",
			err:      errTimeoutConfig,
			expected: false,
		},
		{
			name:     "random error",
			err:      errSomethingWentWrong,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateLLMContext_PropagatesParentCancellation(t *testing.T) {
	logger := zerolog.Nop()
	e := NewExtractor(&logger)
	e.SetLLMTimeout(1 * time.Second)

	// Create a parent context that we'll cancel
	parentCtx, parentCancel := context.WithCancel(context.Background())

	// Create LLM context from active parent
	llmCtx, llmCancel := e.createLLMContext(parentCtx)
	defer llmCancel()

	// LLM context should be active
	require.NoError(t, llmCtx.Err(), "LLM context should be active initially")

	// Cancel parent
	parentCancel()

	// Wait for AfterFunc to propagate cancellation
	select {
	case <-llmCtx.Done():
		// Expected: LLM context should be canceled
	case <-time.After(100 * time.Millisecond):
		t.Error("LLM context should be canceled when parent is canceled")
	}
}

func TestCreateLLMContext_Cleanup(t *testing.T) {
	logger := zerolog.Nop()
	e := NewExtractor(&logger)
	e.SetLLMTimeout(1 * time.Second)

	parentCtx := context.Background()

	llmCtx, llmCancel := e.createLLMContext(parentCtx)

	// Context should be active
	require.NoError(t, llmCtx.Err())

	// Cancel should work
	llmCancel()
	require.Error(t, llmCtx.Err())
}
