package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/redis/go-redis/v9"

	"github.com/example/distributed-api-limiter/internal/api"
	"github.com/example/distributed-api-limiter/internal/config"
	"github.com/example/distributed-api-limiter/internal/limiter"
	"github.com/example/distributed-api-limiter/internal/metrics"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisClient.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		logger.Error("cannot connect to Redis", "address", cfg.RedisAddr, "error", err)
		os.Exit(1)
	}

	rateLimiter, err := limiter.New(cfg.ActiveAlgorithm, redisClient)
	if err != nil {
		logger.Error("cannot create rate limiter", "error", err)
		os.Exit(1)
	}

	metricRecorder := metrics.NewRedisRecorder(redisClient)
	handler := api.NewHandler(cfg, rateLimiter, metricRecorder, redisClient, logger)
	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("rate limiter started", "address", cfg.HTTPAddress, "algorithm", cfg.ActiveAlgorithm)
		serverErrors <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-signalCtx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
