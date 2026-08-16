package limiter

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/redis/go-redis/v9"
)

const slidingWindowLua = `
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local member = ARGV[3]
local redis_time = redis.call('TIME')
local now_ms = (tonumber(redis_time[1]) * 1000) + math.floor(tonumber(redis_time[2]) / 1000)
local cutoff = now_ms - window_ms

redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)
local count = redis.call('ZCARD', KEYS[1])
local allowed = 0
local retry_ms = 0

if count < limit then
  redis.call('ZADD', KEYS[1], now_ms, member)
  count = count + 1
  allowed = 1
else
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  if oldest[2] ~= nil then
    retry_ms = math.max(1, window_ms - (now_ms - tonumber(oldest[2])))
  end
end

redis.call('PEXPIRE', KEYS[1], window_ms + 1000)
return {allowed, math.max(0, limit - count), retry_ms}
`

func newSlidingWindow(client redis.UniversalClient) RateLimiter {
	return &scriptLimiter{
		client:    client,
		script:    redis.NewScript(slidingWindowLua),
		keyPrefix: "rate_limit:sliding:",
		memberID:  randomMemberID,
	}
}

func randomMemberID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
