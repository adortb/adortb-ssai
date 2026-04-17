package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adortb/adortb-ssai/internal/api"
	"github.com/adortb/adortb-ssai/internal/tracking"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	redisAddr := envOr("REDIS_ADDR", "localhost:6379")
	adxBaseURL := envOr("ADX_BASE_URL", "http://localhost:8100")
	selfBaseURL := envOr("SELF_BASE_URL", "https://adx-ssai.adortb.com")
	slateBaseURL := envOr("SLATE_BASE_URL", "https://cdn.adortb.com")
	port := envOr("PORT", "8107")

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Warn("redis ping failed, sessions will be in-memory only", "err", err)
	}

	sessions := tracking.NewSessionStore(rdb)
	cfg := api.Config{
		SelfBaseURL:    selfBaseURL,
		AdxBaseURL:     adxBaseURL,
		SlateBaseURL:   slateBaseURL,
		SegDurationSec: 10,
	}
	handler := api.NewHandler(cfg, sessions)
	router := api.NewRouter(handler)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("adortb-ssai starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down...")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	fmt.Println("stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
