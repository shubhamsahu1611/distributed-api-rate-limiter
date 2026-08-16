package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/example/distributed-api-limiter/internal/config"
	"github.com/example/distributed-api-limiter/internal/limiter"
	"github.com/example/distributed-api-limiter/internal/metrics"
)

type fakeLimiter struct {
	decision limiter.Decision
	err      error
	calls    int
}

func (f *fakeLimiter) Allow(context.Context, string, int64, time.Duration) (limiter.Decision, error) {
	f.calls++
	return f.decision, f.err
}

type fakeMetrics struct {
	recorded []bool
	snapshot metrics.Snapshot
}

func (f *fakeMetrics) Record(_ context.Context, allowed bool) error {
	f.recorded = append(f.recorded, allowed)
	return nil
}

func (f *fakeMetrics) LastHour(context.Context) (metrics.Snapshot, error) {
	return f.snapshot, nil
}

func testHandler(rateLimiter limiter.RateLimiter, recorder metrics.Recorder, failOpen bool) http.Handler {
	cfg := config.Config{
		ActiveAlgorithm: config.AlgorithmTokenBucket,
		FailOpen:        failOpen,
		APIKeys: map[string]config.Tier{
			"test-key": {Name: "FREE", Limit: 10, Window: time.Minute},
		},
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(cfg, rateLimiter, recorder, redisClient, logger)
}

func TestRateLimitAllowsRequest(t *testing.T) {
	rateLimiter := &fakeLimiter{decision: limiter.Decision{Allowed: true, Remaining: 9}}
	recorder := &fakeMetrics{}
	handler := testHandler(rateLimiter, recorder, false)
	request := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	request.Header.Set("x-api-key", "test-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("X-RateLimit-Remaining"); got != "9" {
		t.Fatalf("remaining header = %q, want 9", got)
	}
	if len(recorder.recorded) != 1 || !recorder.recorded[0] {
		t.Fatal("accepted request was not recorded")
	}
}

func TestRateLimitBlocksRequest(t *testing.T) {
	rateLimiter := &fakeLimiter{decision: limiter.Decision{Allowed: false, RetryAfter: 1500 * time.Millisecond}}
	recorder := &fakeMetrics{}
	handler := testHandler(rateLimiter, recorder, false)
	request := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	request.Header.Set("x-api-key", "test-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}
	if got := response.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
	if len(recorder.recorded) != 1 || recorder.recorded[0] {
		t.Fatal("blocked request was not recorded")
	}
}

func TestUnknownAPIKeyIsRejectedBeforeRedis(t *testing.T) {
	rateLimiter := &fakeLimiter{decision: limiter.Decision{Allowed: true}}
	handler := testHandler(rateLimiter, &fakeMetrics{}, false)
	request := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if rateLimiter.calls != 0 {
		t.Fatalf("limiter calls = %d, want 0", rateLimiter.calls)
	}
}

func TestLimiterFailureIsFailClosedByDefault(t *testing.T) {
	rateLimiter := &fakeLimiter{err: errors.New("redis unavailable")}
	handler := testHandler(rateLimiter, &fakeMetrics{}, false)
	request := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	request.Header.Set("x-api-key", "test-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	recorder := &fakeMetrics{snapshot: metrics.Snapshot{Accepted: 80, Blocked: 20}}
	handler := testHandler(&fakeLimiter{}, recorder, false)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Body.String(); got != "{\"accepted\":80,\"blocked\":20,\"drop_rate\":\"20.0%\",\"total\":100,\"window\":\"last_60_minutes\"}\n" {
		t.Fatalf("body = %s", got)
	}
}
