package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/webhook"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Required environment variables
	databaseURL := requireEnv("DATABASE_URL")
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	if geminiAPIKey == "" {
		logger.Warn("GEMINI_API_KEY not set, LLM features will be unavailable")
	}
	appIDStr := requireEnv("GITHUB_APP_ID")
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		logger.Error("GITHUB_APP_ID must be a valid integer", "error", err)
		os.Exit(1)
	}
	privateKeyRaw := requireEnv("GITHUB_PRIVATE_KEY")
	privateKey, err := base64.StdEncoding.DecodeString(privateKeyRaw)
	if err != nil {
		// Not valid base64, treat as raw PEM
		privateKey = []byte(privateKeyRaw)
	}
	webhookSecret := requireEnv("WEBHOOK_SECRET")

	sourceRepo := os.Getenv("SOURCE_REPO")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to database
	pool, err := store.ConnectPool(ctx, databaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	s := store.New(pool)
	if err := s.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Clean up old webhook delivery records (30-day retention)
	if deleted, err := s.CleanupOldDeliveries(ctx, 30*24*time.Hour); err != nil {
		logger.Error("cleanup old deliveries failed", "error", err)
	} else if deleted > 0 {
		logger.Info("cleaned up old deliveries", "deleted", deleted)
	}

	// Initialize clients
	llmClient := llm.New(geminiAPIKey, logger)
	ghClient := gh.New(appID, privateKey)

	// Parse shadow repos configuration
	shadowRepos := parseShadowRepos(os.Getenv("SHADOW_REPOS"))
	if len(shadowRepos) > 0 {
		logger.Info("shadow repos configured", "count", len(shadowRepos))
	}

	// Silent mode: store triage results without posting comments (default: true)
	silentMode := os.Getenv("SILENT_MODE") != "false"
	logger.Info("silent mode", "enabled", silentMode)

	// Set up HTTP server
	handler := webhook.New(webhookSecret, sourceRepo, silentMode, s, llmClient, ghClient, logger, ctx, shadowRepos)

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", handler.ServeHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Ping(r.Context()); err != nil {
			http.Error(w, "database unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	allowedRepos := map[string]bool{"IsmaelMartinez/teams-for-linux": true}
	if sourceRepo != "" {
		allowedRepos[sourceRepo] = true
	}
	for _, shadow := range shadowRepos {
		allowedRepos[shadow] = true
	}
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		stats, err := s.GetDashboardStats(r.Context(), repo)
		if err != nil {
			http.Error(w, "failed to get stats", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(stats); err != nil {
			logger.Error("encoding stats response", "error", err)
		}
	})

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
		logger.Info("waiting for in-flight triage to complete")
		handler.Wait()
		cancel()
	}()

	logger.Info("starting server", "port", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		fmt.Fprintf(os.Stderr, "required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return val
}

func parseShadowRepos(s string) map[string]string {
	result := make(map[string]string)
	if s == "" {
		return result
	}
	for _, pair := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}
