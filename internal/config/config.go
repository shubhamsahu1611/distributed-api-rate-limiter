package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	AlgorithmTokenBucket   = "TOKEN_BUCKET"
	AlgorithmSlidingWindow = "SLIDING_WINDOW"
)

type Tier struct {
	Name   string
	Limit  int64
	Window time.Duration
}

type Config struct {
	HTTPAddress     string
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	ActiveAlgorithm string
	FailOpen        bool
	APIKeys         map[string]Tier
}

func Load() (Config, error) {
	port := envOrDefault("PORT", "8080")
	algorithm := strings.ToUpper(envOrDefault("ACTIVE_ALGORITHM", AlgorithmTokenBucket))
	if algorithm != AlgorithmTokenBucket && algorithm != AlgorithmSlidingWindow {
		return Config{}, fmt.Errorf("ACTIVE_ALGORITHM must be %s or %s", AlgorithmTokenBucket, AlgorithmSlidingWindow)
	}

	redisDB, err := parseNonNegativeInt("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	freeLimit, err := parsePositiveInt64("FREE_LIMIT", 10)
	if err != nil {
		return Config{}, err
	}
	premiumLimit, err := parsePositiveInt64("PREMIUM_LIMIT", 100)
	if err != nil {
		return Config{}, err
	}
	window, err := time.ParseDuration(envOrDefault("RATE_LIMIT_WINDOW", "1m"))
	if err != nil || window < time.Millisecond {
		return Config{}, fmt.Errorf("RATE_LIMIT_WINDOW must be at least 1ms and use Go duration syntax such as 1m")
	}
	failOpen, err := strconv.ParseBool(envOrDefault("FAIL_OPEN", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("FAIL_OPEN must be true or false: %w", err)
	}

	freeKey := envOrDefault("FREE_API_KEY", "free_demo_key")
	premiumKey := envOrDefault("PREMIUM_API_KEY", "premium_demo_key")
	if freeKey == premiumKey {
		return Config{}, fmt.Errorf("FREE_API_KEY and PREMIUM_API_KEY must be different")
	}

	return Config{
		HTTPAddress:     ":" + port,
		RedisAddr:       envOrDefault("REDIS_ADDR", "localhost:6379"),
		RedisPassword:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		ActiveAlgorithm: algorithm,
		FailOpen:        failOpen,
		APIKeys: map[string]Tier{
			freeKey:    {Name: "FREE", Limit: freeLimit, Window: window},
			premiumKey: {Name: "PREMIUM", Limit: premiumLimit, Window: window},
		},
	}, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func parseNonNegativeInt(name string, fallback int) (int, error) {
	value, err := strconv.Atoi(envOrDefault(name, strconv.Itoa(fallback)))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func parsePositiveInt64(name string, fallback int64) (int64, error) {
	value, err := strconv.ParseInt(envOrDefault(name, strconv.FormatInt(fallback, 10)), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}
