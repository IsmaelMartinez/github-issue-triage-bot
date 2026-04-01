package main

import (
	"log/slog"
	"os"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp/tools"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	baseURL := os.Getenv("TRIAGE_BOT_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	secret := os.Getenv("INGEST_SECRET")

	server := mcp.NewServer("triage-bot", "1.0.0")

	healthTool := tools.NewHealthStatusTool(baseURL, secret)
	trendsTool := tools.NewReportTrendsTool(baseURL, secret)
	pendingTool := tools.NewPendingTriageTool(baseURL, secret)
	briefingTool := tools.NewSynthesisBriefingTool(baseURL, secret)

	server.RegisterTool(healthTool.Def, healthTool.Handler)
	server.RegisterTool(trendsTool.Def, trendsTool.Handler)
	server.RegisterTool(pendingTool.Def, pendingTool.Handler)
	server.RegisterTool(briefingTool.Def, briefingTool.Handler)

	logger.Info("triage-bot MCP server starting", "base_url", baseURL, "tools", 4)

	if err := server.Run(os.Stdin, os.Stdout); err != nil {
		logger.Error("MCP server error", "error", err)
		os.Exit(1)
	}
}
