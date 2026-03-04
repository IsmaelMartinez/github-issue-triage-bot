package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Provider defines the LLM operations that triage phases require.
type Provider interface {
	GenerateJSON(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, error)
	GenerateJSONWithSystem(ctx context.Context, systemPrompt, userContent string, temperature float64, maxTokens int) (string, error)
	Embed(ctx context.Context, text string) ([]float32, error)
}

const (
	defaultBaseURL       = "https://generativelanguage.googleapis.com/v1beta"
	defaultHTTPTimeout   = 60 * time.Second
	generationModel      = "gemini-2.5-flash"
	embeddingModel       = "gemini-embedding-001"
	embeddingDimensionality = 768
	maxErrorBodyBytes    = 4096
)

// Client wraps the Gemini API for text generation and embeddings.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
}

// New creates a new LLM client.
func New(apiKey string, logger *slog.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		baseURL: defaultBaseURL,
		logger:  logger,
	}
}

// GenerateJSON sends a prompt to Gemini and returns the raw JSON response text.
func (c *Client) GenerateJSON(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, error) {
	start := time.Now()
	c.logger.Info("llm GenerateJSON start")
	defer func() {
		c.logger.Info("llm GenerateJSON complete", "duration", time.Since(start))
	}()

	body := geminiRequest{
		Contents: []content{
			{Parts: []part{{Text: prompt}}},
		},
		GenerationConfig: generationConfig{
			Temperature:      temperature,
			MaxOutputTokens:  maxTokens,
			ResponseMimeType: "application/json",
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, generationModel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}

// GenerateJSONWithSystem sends user content to Gemini with a trusted system instruction
// and returns the raw JSON response text.
func (c *Client) GenerateJSONWithSystem(ctx context.Context, systemPrompt, userContent string, temperature float64, maxTokens int) (string, error) {
	start := time.Now()
	c.logger.Info("llm GenerateJSONWithSystem start")
	defer func() {
		c.logger.Info("llm GenerateJSONWithSystem complete", "duration", time.Since(start))
	}()

	body := geminiRequestWithSystem{
		SystemInstruction: &content{
			Parts: []part{{Text: systemPrompt}},
		},
		Contents: []content{
			{Parts: []part{{Text: userContent}}},
		},
		GenerationConfig: generationConfig{
			Temperature:      temperature,
			MaxOutputTokens:  maxTokens,
			ResponseMimeType: "application/json",
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, generationModel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}

// Embed generates an embedding for the given text using Gemini's embedding model.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	c.logger.Info("llm Embed start")
	defer func() {
		c.logger.Info("llm Embed complete", "duration", time.Since(start))
	}()

	body := embeddingRequest{
		Model: "models/" + embeddingModel,
		Content: content{
			Parts: []part{{Text: text}},
		},
		OutputDimensionality: embeddingDimensionality,
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:embedContent", c.baseURL, embeddingModel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("embedding API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Embedding.Values, nil
}

// Gemini API types

type geminiRequest struct {
	Contents         []content        `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type geminiRequestWithSystem struct {
	SystemInstruction *content         `json:"systemInstruction,omitempty"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature      float64 `json:"temperature"`
	MaxOutputTokens  int     `json:"maxOutputTokens"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates []candidate `json:"candidates"`
}

type candidate struct {
	Content content `json:"content"`
}

type embeddingRequest struct {
	Model                string  `json:"model"`
	Content              content `json:"content"`
	OutputDimensionality int     `json:"outputDimensionality"`
}

type embeddingResponse struct {
	Embedding embeddingValues `json:"embedding"`
}

type embeddingValues struct {
	Values []float32 `json:"values"`
}
