package enrichment

import (
	"context"
	"errors"
	"fmt"
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
	errSomethingWentWrong = errors.New("something went wrong")
	errTimeoutConfig      = errors.New("feature timeout configuration invalid")
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
		errorUntil: 2, // Fail first 2 attempts, succeed on 3rd
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
			name:     "wrapped context.DeadlineExceeded",
			err:      fmt.Errorf("wrapped error: %w", context.DeadlineExceeded),
			expected: true,
		},
		{
			name:     "random error",
			err:      errSomethingWentWrong,
			expected: false,
		},
		{
			name:     "string containing timeout word is not retryable",
			err:      errTimeoutConfig,
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

func TestCreateLLMContext_AlreadyCanceledParent(t *testing.T) {
	logger := zerolog.Nop()
	e := NewExtractor(&logger)
	e.SetLLMTimeout(1 * time.Second)

	// Create an already-canceled parent context
	parentCtx, parentCancel := context.WithCancel(context.Background())
	parentCancel() // Cancel immediately

	// Create LLM context from already-canceled parent
	llmCtx, llmCancel := e.createLLMContext(parentCtx)
	defer llmCancel()

	// Wait for AfterFunc to execute (it runs on parent.Done() which is already closed)
	select {
	case <-llmCtx.Done():
		// Expected: LLM context should be canceled since parent was already canceled
	case <-time.After(100 * time.Millisecond):
		t.Error("LLM context should be canceled when parent is already canceled")
	}
}

func TestSleepWithContext_Interruption(t *testing.T) {
	logger := zerolog.Nop()
	e := NewExtractor(&logger)

	ctx, cancel := context.WithCancel(context.Background())

	// Start sleep in goroutine
	errCh := make(chan error, 1)
	start := time.Now()

	go func() {
		errCh <- e.sleepWithContext(ctx, 5*time.Second)
	}()

	// Cancel after short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Sleep should return quickly with context error
	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context done")

		elapsed := time.Since(start)
		assert.Less(t, elapsed, 500*time.Millisecond, "sleep should have been interrupted quickly")
	case <-time.After(1 * time.Second):
		t.Fatal("sleepWithContext did not return after context cancellation")
	}
}

func TestSleepWithContext_CompletesNormally(t *testing.T) {
	logger := zerolog.Nop()
	e := NewExtractor(&logger)

	start := time.Now()
	err := e.sleepWithContext(context.Background(), 50*time.Millisecond)
	require.NoError(t, err)

	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond, "sleep should wait at least the specified duration")
}

func TestAddJitter(t *testing.T) {
	baseDuration := 1 * time.Second

	// Run multiple times to verify randomness and bounds
	for i := 0; i < 100; i++ {
		result := addJitter(baseDuration)

		// Result should be at least base duration
		assert.GreaterOrEqual(t, result, baseDuration, "jittered duration should be >= base")

		// Result should be at most base + 30% jitter
		maxExpected := baseDuration + time.Duration(float64(baseDuration)*llmRetryJitterRatio)
		assert.LessOrEqual(t, result, maxExpected, "jittered duration should be <= base + max jitter")
	}
}

// timeoutError implements net.Error for testing.
type timeoutError struct {
	timeout bool
}

func (e *timeoutError) Error() string {
	return "network timeout error"
}

func (e *timeoutError) Timeout() bool {
	return e.timeout
}

func (e *timeoutError) Temporary() bool {
	return false
}

func TestIsRetryableError_NetError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "net.Error with Timeout() true",
			err:      &timeoutError{timeout: true},
			expected: true,
		},
		{
			name:     "net.Error with Timeout() false",
			err:      &timeoutError{timeout: false},
			expected: false,
		},
		{
			name:     "wrapped net.Error with Timeout() true",
			err:      fmt.Errorf("wrapped: %w", &timeoutError{timeout: true}),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// delayTrackingMockLLMClient tracks call timestamps for backoff verification.
type delayTrackingMockLLMClient struct {
	llm.Client
	callTimes  []time.Time
	errorUntil int32
	callCount  atomic.Int32
	response   string
}

func (m *delayTrackingMockLLMClient) CompleteText(_ context.Context, _, _ string) (string, error) {
	m.callTimes = append(m.callTimes, time.Now())
	count := m.callCount.Add(1)

	if count <= m.errorUntil {
		return "", context.DeadlineExceeded
	}

	return m.response, nil
}

func TestExtractor_BackoffTiming(t *testing.T) {
	logger := zerolog.Nop()
	m := &delayTrackingMockLLMClient{
		errorUntil: 2, // Fail first 2 attempts, succeed on 3rd
		response:   `[{"text": "Claim 1", "entities": []}]`,
	}

	e := NewExtractor(&logger)
	e.SetLLMClient(m, testModel)
	e.SetLLMTimeout(50 * time.Millisecond)

	_, err := e.extractClaimsWithLLM(context.Background(), testContentForLLM)
	require.NoError(t, err)
	require.Len(t, m.callTimes, 3, "expected 3 calls")

	// Verify delays between calls
	// First retry should have ~2s delay (base delay)
	firstDelay := m.callTimes[1].Sub(m.callTimes[0])
	// Second retry should have ~4s delay (2x backoff)
	secondDelay := m.callTimes[2].Sub(m.callTimes[1])

	// Allow for jitter: delays should be at least base and at most base + 30%
	minFirstDelay := llmRetryDelay
	maxFirstDelay := llmRetryDelay + time.Duration(float64(llmRetryDelay)*llmRetryJitterRatio) + 100*time.Millisecond

	minSecondDelay := llmRetryDelay * llmRetryBackoffMult
	maxSecondDelay := minSecondDelay + time.Duration(float64(minSecondDelay)*llmRetryJitterRatio) + 100*time.Millisecond

	assert.GreaterOrEqual(t, firstDelay, minFirstDelay, "first retry delay should be >= base delay")
	assert.LessOrEqual(t, firstDelay, maxFirstDelay, "first retry delay should be <= base + jitter")

	assert.GreaterOrEqual(t, secondDelay, minSecondDelay, "second retry delay should be >= 2x base delay")
	assert.LessOrEqual(t, secondDelay, maxSecondDelay, "second retry delay should be <= 2x base + jitter")
}
