package limiter

import (
	"testing"
	"time"
)

func TestRedisKeyDoesNotExposeSubject(t *testing.T) {
	const subject = "a-sensitive-api-key"
	first := redisKey("rate_limit:", subject)
	second := redisKey("rate_limit:", subject)

	if first != second {
		t.Fatal("redisKey() must be deterministic")
	}
	if first == "rate_limit:"+subject {
		t.Fatal("redisKey() exposed the API key")
	}
}

func TestParseScriptResult(t *testing.T) {
	decision, err := parseScriptResult([]interface{}{int64(0), int64(0), int64(1250)})
	if err != nil {
		t.Fatalf("parseScriptResult() error = %v", err)
	}
	if decision.Allowed {
		t.Fatal("Allowed = true, want false")
	}
	if decision.RetryAfter != 1250*time.Millisecond {
		t.Fatalf("RetryAfter = %s, want 1.25s", decision.RetryAfter)
	}
}
