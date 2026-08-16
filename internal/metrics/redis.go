package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const recordMetricLua = `
redis.call('HINCRBY', KEYS[1], ARGV[1], 1)
redis.call('EXPIRE', KEYS[1], 7200)
return 1
`

type Snapshot struct {
	Accepted int64
	Blocked  int64
}

type Recorder interface {
	Record(ctx context.Context, allowed bool) error
	LastHour(ctx context.Context) (Snapshot, error)
}

type RedisRecorder struct {
	client redis.UniversalClient
	script *redis.Script
	now    func() time.Time
}

func NewRedisRecorder(client redis.UniversalClient) *RedisRecorder {
	return &RedisRecorder{
		client: client,
		script: redis.NewScript(recordMetricLua),
		now:    time.Now,
	}
}

func (r *RedisRecorder) Record(ctx context.Context, allowed bool) error {
	field := "blocked"
	if allowed {
		field = "accepted"
	}
	return r.script.Run(ctx, r.client, []string{minuteKey(r.now())}, field).Err()
}

func (r *RedisRecorder) LastHour(ctx context.Context) (Snapshot, error) {
	pipe := r.client.Pipeline()
	commands := make([]*redis.MapStringStringCmd, 0, 60)
	now := r.now()
	for offset := 0; offset < 60; offset++ {
		commands = append(commands, pipe.HGetAll(ctx, minuteKey(now.Add(-time.Duration(offset)*time.Minute))))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return Snapshot{}, err
	}

	var snapshot Snapshot
	for _, command := range commands {
		values, err := command.Result()
		if err != nil && err != redis.Nil {
			return Snapshot{}, err
		}
		if value := values["accepted"]; value != "" {
			var accepted int64
			if _, err := fmt.Sscan(value, &accepted); err != nil {
				return Snapshot{}, fmt.Errorf("parse accepted metric: %w", err)
			}
			snapshot.Accepted += accepted
		}
		if value := values["blocked"]; value != "" {
			var blocked int64
			if _, err := fmt.Sscan(value, &blocked); err != nil {
				return Snapshot{}, fmt.Errorf("parse blocked metric: %w", err)
			}
			snapshot.Blocked += blocked
		}
	}
	return snapshot, nil
}

func minuteKey(value time.Time) string {
	return "metrics:requests:" + value.UTC().Format("200601021504")
}
