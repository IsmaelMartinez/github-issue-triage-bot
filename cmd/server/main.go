package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mirror"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/synthesis"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/webhook"
)

// server bundles the long-lived dependencies that request handlers need.
// Handlers added as methods on *server can reach the store, GitHub client,
// LLM client, logger, and the set of repos the deployment is configured to
// handle without threading each of them through a closure.
type server struct {
	store        *store.Store
	gh           *gh.Client
	llm          llm.Provider
	logger       *slog.Logger
	allowedRepos map[string]bool
}

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
	ingestSecret := os.Getenv("INGEST_SECRET")
	// Cron-triggered endpoints (/cleanup, /health-check, /ingest, /synthesize, /pause, /unpause,
	// /upstream-watch, /brief-preview) rely on either (a) INGEST_SECRET being empty with Cloud Run's
	// IAM layer handling auth, or (b) setting INGEST_SECRET and having callers send
	// "Authorization: Bearer <INGEST_SECRET>". The Workload-Identity-token pattern used by the
	// GitHub Actions workflows only works in mode (a).
	if ingestSecret == "" {
		logger.Warn("INGEST_SECRET not set — /cleanup, /health-check, /ingest, /synthesize, /pause, /unpause, /upstream-watch, /brief-preview are unauthenticated at app layer (Cloud Run IAM may still gate)")
	}

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

	if err := store.Migrate(ctx, pool, logger); err != nil {
		logger.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}

	// Clean up old webhook delivery records (30-day retention)
	if deleted, err := s.CleanupOldDeliveries(ctx, 30*24*time.Hour); err != nil {
		logger.Error("cleanup old deliveries failed", "error", err)
	} else if deleted > 0 {
		logger.Info("cleaned up old deliveries", "deleted", deleted)
	}

	// Clean up old event journal entries (180-day retention)
	if deleted, err := s.CleanupOldEvents(ctx, 180*24*time.Hour); err != nil {
		logger.Error("cleanup old events failed", "error", err)
	} else if deleted > 0 {
		logger.Info("cleaned up old events", "deleted", deleted)
	}

	// Initialize clients
	llmClient := llm.New(geminiAPIKey, logger)
	if envLimit := os.Getenv("MAX_DAILY_LLM_CALLS"); envLimit != "" {
		if limit, err := strconv.Atoi(envLimit); err == nil && limit > 0 {
			llmClient.SetDailyLimit(limit)
			logger.Info("LLM daily call limit set from env", "limit", limit)
		}
	}
	ghClient := gh.New(appID, privateKey)

	// Parse shadow repos configuration
	shadowRepos := parseShadowRepos(os.Getenv("SHADOW_REPOS"))
	if len(shadowRepos) > 0 {
		logger.Info("shadow repos configured", "count", len(shadowRepos))
	}

	// Set up mirror service for shadow repo code sync
	var mirrorSvc *mirror.Service
	if len(shadowRepos) > 0 {
		mirrorCacheDir := os.Getenv("MIRROR_CACHE_DIR")
		if mirrorCacheDir == "" {
			mirrorCacheDir = os.TempDir()
		}
		mirrorSvc = mirror.New(ghClient, logger, mirrorCacheDir)
		logger.Info("mirror service configured", "cacheDir", mirrorCacheDir)
	}

	// Set up HTTP server
	handler := webhook.New(webhookSecret, sourceRepo, s, llmClient, ghClient, logger, ctx, shadowRepos, mirrorSvc)

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
	allowedRepos := map[string]bool{
		"IsmaelMartinez/teams-for-linux":         true,
		"IsmaelMartinez/github-issue-triage-bot": true, // default alert_repo for /health-check
	}
	if sourceRepo != "" {
		allowedRepos[sourceRepo] = true
	}
	for source, shadow := range shadowRepos {
		allowedRepos[source] = true
		allowedRepos[shadow] = true
	}

	srv := &server{
		store:        s,
		gh:           ghClient,
		llm:          llmClient,
		logger:       logger,
		allowedRepos: allowedRepos,
	}
	mux.HandleFunc("/cleanup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		staleDuration := 14 * 24 * time.Hour
		stale, err := s.ListStaleSessions(r.Context(), staleDuration)
		if err != nil {
			logger.Error("failed to list stale sessions", "error", err)
			http.Error(w, fmt.Sprintf("failed to list stale sessions: %v", err), http.StatusInternalServerError)
			return
		}

		if len(stale) == 0 {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"closed":0}`)
			return
		}

		// Get installation ID for closing issues
		installations, err := ghClient.ListInstallations(r.Context())
		if err != nil || len(installations) == 0 {
			logger.Error("failed to get installations for cleanup", "error", err)
			http.Error(w, "failed to get installations", http.StatusInternalServerError)
			return
		}
		installID := installations[0]

		closed := 0
		for _, ss := range stale {
			// Orphaned agent sessions have ShadowIssueNumber == 0 (NULL in the DB);
			// there's no shadow issue to close, so just mark the session complete.
			if ss.ShadowIssueNumber == 0 {
				if ss.SessionType == "agent" {
					_ = s.MarkSessionComplete(r.Context(), ss.ID)
				}
				closed++
				logger.Info("closed orphaned agent session with no shadow issue", "session_id", ss.ID, "shadow_repo", ss.ShadowRepo)
				continue
			}
			note := "This shadow issue has been automatically closed after 14 days without a response."
			if _, err := ghClient.CreateComment(r.Context(), installID, ss.ShadowRepo, ss.ShadowIssueNumber, note); err != nil {
				logger.Error("failed to comment on stale shadow issue", "error", err, "shadow_repo", ss.ShadowRepo, "shadow_issue", ss.ShadowIssueNumber)
				continue
			}
			if err := ghClient.CloseIssue(r.Context(), installID, ss.ShadowRepo, ss.ShadowIssueNumber); err != nil {
				logger.Error("failed to close stale shadow issue", "error", err, "shadow_repo", ss.ShadowRepo, "shadow_issue", ss.ShadowIssueNumber)
				continue
			}
			switch ss.SessionType {
			case "agent":
				_ = s.MarkSessionComplete(r.Context(), ss.ID)
			case "triage":
				_ = s.MarkTriageSessionClosed(r.Context(), ss.ID)
			}
			closed++
			logger.Info("closed stale shadow issue", "type", ss.SessionType, "shadow_repo", ss.ShadowRepo, "shadow_issue", ss.ShadowIssueNumber)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"closed":%d,"total_stale":%d}`, closed, len(stale))
	})
	mux.HandleFunc("/health-check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		alertRepo := r.URL.Query().Get("alert_repo")
		if alertRepo == "" {
			alertRepo = "IsmaelMartinez/github-issue-triage-bot"
		}
		if !allowedRepos[alertRepo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		metrics, metricsErr := s.GetHealthMetrics(r.Context(), repo)
		if metricsErr != nil {
			logger.Warn("partial health metrics returned", "error", metricsErr, "repo", repo)
		}
		if metrics == nil {
			http.Error(w, "failed to get health metrics", http.StatusInternalServerError)
			return
		}

		alerts := store.EvaluateThresholds(metrics)
		if len(alerts) > 0 {
			logger.Warn("health check alerts", "count", len(alerts), "repo", repo)

			installations, instErr := ghClient.ListInstallations(r.Context())
			if instErr != nil || len(installations) == 0 {
				logger.Error("failed to get installations for health alerts", "error", instErr)
			} else {
				installID := installations[0]
				for _, alert := range alerts {
					title := fmt.Sprintf("[Health Alert] %s (%s)", alert.Metric, repo)
					// Check for existing open alert issue to avoid duplicates
					query := fmt.Sprintf("repo:%s is:open \"%s\" in:title", alertRepo, title)
					existing, searchErr := ghClient.SearchIssues(r.Context(), installID, query)
					if searchErr != nil {
						logger.Error("failed to search for existing alert issue", "error", searchErr, "metric", alert.Metric)
						continue
					}
					if len(existing) > 0 {
						logger.Info("alert issue already exists, skipping", "metric", alert.Metric, "existing_issue", existing[0].Number)
						continue
					}
					body := fmt.Sprintf("## Health Alert: %s\n\nCurrent: %.4f\nThreshold: %.4f\n\n%s\n\nRepo: %s\nChecked at: %s",
						alert.Metric, alert.Current, alert.Threshold, alert.Message, repo, metrics.CheckedAt)
					issueNum, createErr := ghClient.CreateIssue(r.Context(), installID, alertRepo, title, body)
					if createErr != nil {
						logger.Error("failed to create alert issue", "error", createErr, "metric", alert.Metric)
						continue
					}
					logger.Info("created health alert issue", "metric", alert.Metric, "issue", issueNum, "repo", alertRepo)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			Metrics *store.HealthMetrics `json:"metrics"`
			Alerts  []store.HealthAlert  `json:"alerts"`
			Partial bool                 `json:"partial"`
		}{
			Metrics: metrics,
			Alerts:  alerts,
			Partial: metricsErr != nil,
		}
		if resp.Alerts == nil {
			resp.Alerts = []store.HealthAlert{}
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Error("encoding health check response", "error", err)
		}
	})

	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20) // 5 MB limit
		var events []store.RepoEvent
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := s.RecordEvents(r.Context(), events); err != nil {
			logger.Error("ingesting events", "error", err)
			http.Error(w, "ingest failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ingested":%d}`, len(events))
	})

	mux.HandleFunc("/pause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			http.Error(w, "missing repo parameter", http.StatusBadRequest)
			return
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		paused := !strings.HasSuffix(r.URL.Path, "/unpause")
		if err := s.SetPaused(r.Context(), repo, paused, "api"); err != nil {
			http.Error(w, "failed to set pause state", http.StatusInternalServerError)
			return
		}
		state := "paused"
		if !paused {
			state = "unpaused"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"repo":%q,"status":%q}`, repo, state)
	})

	mux.HandleFunc("/unpause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			http.Error(w, "missing repo parameter", http.StatusBadRequest)
			return
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := s.SetPaused(r.Context(), repo, false, "api"); err != nil {
			http.Error(w, "failed to set pause state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"repo":%q,"status":"unpaused"}`, repo)
	})

	mux.HandleFunc("/synthesize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" || !allowedRepos[repo] {
			http.Error(w, "invalid or missing repo parameter", http.StatusBadRequest)
			return
		}
		shadowRepo, hasShadow := shadowRepos[repo]
		if !hasShadow {
			http.Error(w, "no shadow repo configured for this repo", http.StatusBadRequest)
			return
		}

		installations, instErr := ghClient.ListInstallations(r.Context())
		if instErr != nil {
			logger.Error("failed to get installations for synthesis", "error", instErr)
			http.Error(w, "failed to get installations", http.StatusInternalServerError)
			return
		}
		if len(installations) == 0 {
			http.Error(w, "no installations found", http.StatusInternalServerError)
			return
		}
		installID := installations[0]

		clusterSynth := synthesis.NewClusterSynthesizer(s)
		driftSynth := synthesis.NewDriftSynthesizer(s)
		upstreamSynth := synthesis.NewUpstreamSynthesizer(s)
		runner := synthesis.NewRunner(ghClient, s, logger, clusterSynth, driftSynth, upstreamSynth)

		const weeklyLookback = 7 * 24 * time.Hour
		findingCount, runErr := runner.Run(r.Context(), installID, repo, shadowRepo, weeklyLookback)
		if runErr != nil {
			logger.Error("synthesis run failed", "error", runErr, "repo", repo)
			http.Error(w, "synthesis failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","findings":%d}`, findingCount)
	})

	mux.HandleFunc("/upstream-watch", func(w http.ResponseWriter, r *http.Request) {
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		srv.upstreamWatchHandler(w, r)
	})

	mux.HandleFunc("/brief-preview", func(w http.ResponseWriter, r *http.Request) {
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		srv.briefPreviewHandler(w, r)
	})

	// Live dashboard
	sortedRepos := make([]string, 0, len(allowedRepos))
	for r := range allowedRepos {
		sortedRepos = append(sortedRepos, r)
	}
	sort.Strings(sortedRepos)
	dashTmpl := mustParseDashboard()
	mux.HandleFunc("/dashboard", newDashboardHandler(s, allowedRepos, sortedRepos, dashTmpl, logger))

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
	mux.HandleFunc("/report/trends", func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		weeks := parseWeeksParam(r.URL.Query().Get("weeks"))
		trends, trendsErr := s.GetWeeklyTrends(r.Context(), repo, weeks)
		if trends == nil {
			http.Error(w, "failed to get trends", http.StatusInternalServerError)
			return
		}
		since := time.Now().Add(-30 * 24 * time.Hour)
		findings, findingsErr := s.GetRecentFindings(r.Context(), repo, since)
		if findingsErr != nil {
			logger.Warn("failed to get recent findings", "error", findingsErr, "repo", repo)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			*store.WeeklyTrends
			Partial   bool                    `json:"partial"`
			Synthesis *store.SynthesisFindings `json:"synthesis_findings,omitempty"`
		}{
			WeeklyTrends: trends,
			Partial:      trendsErr != nil,
			Synthesis:    findings,
		}
		if trendsErr != nil {
			logger.Warn("partial weekly trends", "error", trendsErr, "repo", repo)
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Error("encoding trends response", "error", err)
		}
	})
	mux.HandleFunc("/api/triage/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		issueStr := strings.TrimPrefix(r.URL.Path, "/api/triage/")
		issueNum, err := strconv.Atoi(issueStr)
		if err != nil {
			http.Error(w, "invalid issue number", http.StatusBadRequest)
			return
		}
		detail, err := s.GetTriageSessionDetail(r.Context(), repo, issueNum)
		if err != nil {
			logger.Error("fetching triage detail", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if detail == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(detail); encErr != nil {
			logger.Error("encoding triage detail", "error", encErr)
		}
	})
	mux.HandleFunc("/api/agent/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		issueStr := strings.TrimPrefix(r.URL.Path, "/api/agent/")
		issueNum, err := strconv.Atoi(issueStr)
		if err != nil {
			http.Error(w, "invalid issue number", http.StatusBadRequest)
			return
		}
		detail, err := s.GetAgentSessionDetail(r.Context(), repo, issueNum)
		if err != nil {
			logger.Error("fetching agent detail", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if detail == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(detail); encErr != nil {
			logger.Error("encoding agent detail", "error", encErr)
		}
	})

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 120 * time.Second,
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

func validateIngestAuth(authHeader, secret string) bool {
	if secret == "" {
		return true
	}
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || authHeader[:len(prefix)] != prefix {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(authHeader[len(prefix):]), []byte(secret)) == 1
}

func parseWeeksParam(s string) int {
	if s == "" {
		return store.ClampWeeks(0)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return store.ClampWeeks(0)
	}
	return store.ClampWeeks(n)
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
