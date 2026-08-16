package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/example/distributed-api-limiter/internal/config"
	"github.com/example/distributed-api-limiter/internal/limiter"
	"github.com/example/distributed-api-limiter/internal/metrics"
)

type Server struct {
	config  config.Config
	limiter limiter.RateLimiter
	metrics metrics.Recorder
	redis   redis.UniversalClient
	logger  *slog.Logger
}

func NewHandler(
	cfg config.Config,
	rateLimiter limiter.RateLimiter,
	metricRecorder metrics.Recorder,
	redisClient redis.UniversalClient,
	logger *slog.Logger,
) http.Handler {
	server := &Server{
		config:  cfg,
		limiter: rateLimiter,
		metrics: metricRecorder,
		redis:   redisClient,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /metrics", server.metricSnapshot)
	mux.Handle("GET /api/data", server.rateLimit(http.HandlerFunc(server.data)))
	return requestLogger(logger, mux)
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		apiKey := request.Header.Get("x-api-key")
		tier, exists := s.config.APIKeys[apiKey]
		if !exists {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "a valid x-api-key header is required"})
			return
		}

		decision, err := s.limiter.Allow(request.Context(), apiKey, tier.Limit, tier.Window)
		if err != nil {
			s.logger.Error("rate limit check failed", "error", err)
			if !s.config.FailOpen {
				writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "rate limiting service unavailable"})
				return
			}
			decision = limiter.Decision{Allowed: true, Remaining: -1}
		}

		response.Header().Set("X-RateLimit-Limit", strconv.FormatInt(tier.Limit, 10))
		response.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(decision.Remaining, 10))
		response.Header().Set("X-RateLimit-Tier", tier.Name)
		response.Header().Set("X-RateLimit-Algorithm", s.config.ActiveAlgorithm)

		if metricErr := s.metrics.Record(request.Context(), decision.Allowed); metricErr != nil {
			s.logger.Error("metric recording failed", "error", metricErr)
		}

		if !decision.Allowed {
			retrySeconds := int64(math.Ceil(decision.RetryAfter.Seconds()))
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			response.Header().Set("Retry-After", strconv.FormatInt(retrySeconds, 10))
			writeJSON(response, http.StatusTooManyRequests, map[string]any{
				"error":               "rate limit exceeded",
				"retry_after_seconds": retrySeconds,
			})
			return
		}

		next.ServeHTTP(response, request)
	})
}

func (s *Server) data(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"message": "request accepted",
		"data":    []string{"alpha", "beta", "gamma"},
	})
}

func (s *Server) health(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()
	if err := s.redis.Ping(ctx).Err(); err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "redis": "unavailable"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "healthy", "redis": "connected"})
}

func (s *Server) metricSnapshot(response http.ResponseWriter, request *http.Request) {
	snapshot, err := s.metrics.LastHour(request.Context())
	if err != nil {
		s.logger.Error("metric read failed", "error", err)
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "metrics unavailable"})
		return
	}

	total := snapshot.Accepted + snapshot.Blocked
	dropRate := 0.0
	if total > 0 {
		dropRate = float64(snapshot.Blocked) / float64(total) * 100
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"accepted":  snapshot.Accepted,
		"blocked":   snapshot.Blocked,
		"total":     total,
		"drop_rate": strconv.FormatFloat(dropRate, 'f', 1, 64) + "%",
		"window":    "last_60_minutes",
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		logger.Info("request completed",
			"method", request.Method,
			"path", request.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}
