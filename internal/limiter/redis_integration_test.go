package limiter

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestTokenBucketAgainstRedis(t *testing.T) {
	client := integrationRedis(t)
	subject := fmt.Sprintf("token-integration-%d", time.Now().UnixNano())
	key := redisKey("rate_limit:token:", subject)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	rateLimiter := newTokenBucket(client)
	assertAllowedSequence(t, rateLimiter, subject)
}

func TestSlidingWindowAgainstRedis(t *testing.T) {
	client := integrationRedis(t)
	subject := fmt.Sprintf("sliding-integration-%d", time.Now().UnixNano())
	key := redisKey("rate_limit:sliding:", subject)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	rateLimiter := newSlidingWindow(client)
	assertAllowedSequence(t, rateLimiter, subject)
}

func integrationRedis(t *testing.T) *redis.Client {
	t.Helper()
	address := os.Getenv("REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set REDIS_TEST_ADDR to run Redis integration tests")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("connect to test Redis: %v", err)
	}
	return client
}

func assertAllowedSequence(t *testing.T, rateLimiter RateLimiter, subject string) {
	t.Helper()
	ctx := context.Background()
	for requestNumber := 1; requestNumber <= 3; requestNumber++ {
		decision, err := rateLimiter.Allow(ctx, subject, 2, 10*time.Second)
		if err != nil {
			t.Fatalf("request %d: Allow() error = %v", requestNumber, err)
		}
		wantAllowed := requestNumber <= 2
		if decision.Allowed != wantAllowed {
			t.Fatalf("request %d: Allowed = %t, want %t", requestNumber, decision.Allowed, wantAllowed)
		}
		if !decision.Allowed && decision.RetryAfter <= 0 {
			t.Fatalf("request %d: blocked request has no retry delay", requestNumber)
		}
	}
}
