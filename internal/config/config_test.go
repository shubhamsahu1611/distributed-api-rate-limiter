package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	for _, name := range []string{
		"PORT", "REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "ACTIVE_ALGORITHM",
		"FAIL_OPEN", "FREE_LIMIT", "PREMIUM_LIMIT", "RATE_LIMIT_WINDOW",
		"FREE_API_KEY", "PREMIUM_API_KEY",
	} {
		t.Setenv(name, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ActiveAlgorithm != AlgorithmTokenBucket {
		t.Fatalf("algorithm = %q, want %q", cfg.ActiveAlgorithm, AlgorithmTokenBucket)
	}
	if got := cfg.APIKeys["free_demo_key"].Limit; got != 10 {
		t.Fatalf("free limit = %d, want 10", got)
	}
	if got := cfg.APIKeys["premium_demo_key"].Limit; got != 100 {
		t.Fatalf("premium limit = %d, want 100", got)
	}
}

func TestLoadRejectsUnknownAlgorithm(t *testing.T) {
	t.Setenv("ACTIVE_ALGORITHM", "FIXED_WINDOW")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an unsupported algorithm error")
	}
}

func TestLoadCustomLimits(t *testing.T) {
	t.Setenv("ACTIVE_ALGORITHM", "sliding_window")
	t.Setenv("FREE_LIMIT", "25")
	t.Setenv("PREMIUM_LIMIT", "250")
	t.Setenv("RATE_LIMIT_WINDOW", "30s")
	t.Setenv("FREE_API_KEY", "free-test")
	t.Setenv("PREMIUM_API_KEY", "premium-test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ActiveAlgorithm != AlgorithmSlidingWindow {
		t.Fatalf("algorithm = %q, want %q", cfg.ActiveAlgorithm, AlgorithmSlidingWindow)
	}
	if got := cfg.APIKeys["free-test"].Limit; got != 25 {
		t.Fatalf("free limit = %d, want 25", got)
	}
}
