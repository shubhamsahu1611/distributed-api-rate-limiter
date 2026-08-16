package limiter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Decision struct {
	Allowed    bool
	Remaining  int64
	RetryAfter time.Duration
}

type RateLimiter interface {
	Allow(ctx context.Context, subject string, limit int64, window time.Duration) (Decision, error)
}

type scriptLimiter struct {
	client    redis.UniversalClient
	script    *redis.Script
	keyPrefix string
	memberID  func() (string, error)
}

func New(algorithm string, client redis.UniversalClient) (RateLimiter, error) {
	switch algorithm {
	case "TOKEN_BUCKET":
		return newTokenBucket(client), nil
	case "SLIDING_WINDOW":
		return newSlidingWindow(client), nil
	default:
		return nil, fmt.Errorf("unsupported rate limiting algorithm %q", algorithm)
	}
}

func redisKey(prefix, subject string) string {
	digest := sha256.Sum256([]byte(subject))
	return prefix + hex.EncodeToString(digest[:])
}

func parseScriptResult(raw any) (Decision, error) {
	values, ok := raw.([]interface{})
	if !ok || len(values) < 3 {
		return Decision{}, fmt.Errorf("unexpected Redis script result: %T", raw)
	}
	allowed, err := asInt64(values[0])
	if err != nil {
		return Decision{}, err
	}
	remaining, err := asInt64(values[1])
	if err != nil {
		return Decision{}, err
	}
	retryMilliseconds, err := asInt64(values[2])
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Allowed:    allowed == 1,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryMilliseconds) * time.Millisecond,
	}, nil
}

func asInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis number type %T", value)
	}
}
