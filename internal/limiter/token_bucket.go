package limiter

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenBucketLua = `
local capacity = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local redis_time = redis.call('TIME')
local now_ms = (tonumber(redis_time[1]) * 1000) + math.floor(tonumber(redis_time[2]) / 1000)

local state = redis.call('HMGET', KEYS[1], 'tokens', 'updated_at')
local tokens = tonumber(state[1])
local updated_at = tonumber(state[2])

if tokens == nil or updated_at == nil then
  tokens = capacity
  updated_at = now_ms
end

local elapsed = math.max(0, now_ms - updated_at)
local refill_rate = capacity / window_ms
tokens = math.min(capacity, tokens + (elapsed * refill_rate))

local allowed = 0
local retry_ms = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry_ms = math.ceil((1 - tokens) / refill_rate)
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'updated_at', now_ms)
redis.call('PEXPIRE', KEYS[1], math.ceil(window_ms * 2))

return {allowed, math.floor(tokens), retry_ms}
`

func newTokenBucket(client redis.UniversalClient) RateLimiter {
	return &scriptLimiter{
		client:    client,
		script:    redis.NewScript(tokenBucketLua),
		keyPrefix: "rate_limit:token:",
	}
}

func (l *scriptLimiter) Allow(ctx context.Context, subject string, limit int64, window time.Duration) (Decision, error) {
	arguments := []interface{}{limit, window.Milliseconds()}
	if l.memberID != nil {
		member, err := l.memberID()
		if err != nil {
			return Decision{}, err
		}
		arguments = append(arguments, member)
	}

	result, err := l.script.Run(ctx, l.client, []string{redisKey(l.keyPrefix, subject)}, arguments...).Result()
	if err != nil {
		return Decision{}, err
	}
	return parseScriptResult(result)
}
