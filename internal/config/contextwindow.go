package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go-llm-proxy/internal/httputil"
)

// ModelLimits holds the detected limits for a model from the backend.
type ModelLimits struct {
	ContextWindow int
	MaxOutput     int
}

// DetectContextWindows queries each backend's models endpoint to discover
// context window and max output sizes. Results are stored on the ConfigStore's
// model entries. Runs asynchronously — failures are logged but never block startup.
func DetectContextWindows(cs *ConfigStore) {
	cfg := cs.Get()
	client := httputil.NewHTTPClient()
	client.Timeout = 10 * time.Second

	for i := range cfg.Models {
		m := &cfg.Models[i]
		go detectOne(client, cs, m.Name, m.Backend, m.Model, m.APIKey, m.Type, m.ContextWindow, m.MaxOutput)
	}
}

// detectOne runs backend detection for a single model.
//
// Priority rules:
//  1. If the user configured a value (context_window or max_output > 0), that
//     value is used — but clamped to not exceed the backend's reported limit.
//  2. If the user did NOT configure a value, the backend's reported value is
//     used (auto-detect).
//  3. If the backend doesn't report a value either, the field stays at 0
//     (unset = no clamp / no advertised limit).
//
// This means a user can set context_window: 1000000 to override a backend
// that reports 204800, but they CANNOT set context_window: 9999999 if the
// backend reports 1000000 — it will be clamped to 1000000 with a warning.
func detectOne(client *http.Client, cs *ConfigStore, name, backend, modelID, apiKey, backendType string, configuredCtx, configuredMaxOut int) {
	var limits ModelLimits
	var err error

	switch backendType {
	case BackendAnthropic:
		limits, err = detectAnthropic(client, backend, modelID, apiKey)
	case BackendBedrock:
		// Bedrock has no API endpoint that returns model limits;
		// GetFoundationModel reports modality/region but not max tokens,
		// and Mantle's OpenAI-compatible /v1/models omits it too. Fall
		// back to a lookup table of well-known model-ID prefixes. On a
		// miss we leave detection empty and let the configured value
		// stand.
		limits.ContextWindow = lookupBedrockContextWindow(modelID)
	default:
		limits, err = detectOpenAI(client, backend, modelID, apiKey)
	}

	if err != nil {
		if configuredCtx > 0 || configuredMaxOut > 0 {
			slog.Info("model limits detection failed; keeping configured values",
				"model", name, "error", err)
		} else {
			slog.Warn("failed to detect model limits",
				"model", name, "backend", backend, "error", err)
		}
		return
	}

	// Apply priority rules for context_window.
	ctxWindow := limits.ContextWindow
	if configuredCtx > 0 {
		if limits.ContextWindow > 0 && configuredCtx > limits.ContextWindow {
			slog.Warn("configured context_window exceeds backend limit; clamping",
				"model", name, "configured", configuredCtx, "backend_limit", limits.ContextWindow)
			ctxWindow = limits.ContextWindow
		} else {
			ctxWindow = configuredCtx
		}
	}

	// Apply priority rules for max_output.
	maxOutput := limits.MaxOutput
	if configuredMaxOut > 0 {
		if limits.MaxOutput > 0 && configuredMaxOut > limits.MaxOutput {
			slog.Warn("configured max_output exceeds backend limit; clamping",
				"model", name, "configured", configuredMaxOut, "backend_limit", limits.MaxOutput)
			maxOutput = limits.MaxOutput
		} else {
			maxOutput = configuredMaxOut
		}
	}

	// Update the config under the write lock.
	cs.mu.Lock()
	for i := range cs.config.Models {
		if cs.config.Models[i].Name == name {
			if ctxWindow > 0 {
				cs.config.Models[i].ContextWindow = ctxWindow
			}
			if maxOutput > 0 {
				cs.config.Models[i].MaxOutput = maxOutput
			}
			break
		}
	}
	cs.mu.Unlock()

	if ctxWindow > 0 {
		switch {
		case configuredCtx > 0 && configuredCtx != ctxWindow:
			slog.Info("context window clamped to backend limit",
				"model", name, "configured", configuredCtx, "effective", ctxWindow)
		case configuredCtx > 0:
			slog.Info("context window matches configured value",
				"model", name, "context_window", ctxWindow)
		default:
			slog.Info("detected context window",
				"model", name, "context_window", ctxWindow)
		}
	}
	if maxOutput > 0 {
		switch {
		case configuredMaxOut > 0 && configuredMaxOut != maxOutput:
			slog.Info("max output clamped to backend limit",
				"model", name, "configured", configuredMaxOut, "effective", maxOutput)
		case configuredMaxOut > 0:
			slog.Info("max output matches configured value",
				"model", name, "max_output", maxOutput)
		default:
			slog.Info("detected max output",
				"model", name, "max_output", maxOutput)
		}
	}
}

// detectOpenAI queries GET /models on an OpenAI-compatible backend and
// extracts max_model_len and max_output_token_length from the matching model entry.
func detectOpenAI(client *http.Client, backend, modelID, apiKey string) (ModelLimits, error) {
	base := strings.TrimRight(backend, "/")

	// Try llama.cpp /props endpoint first — it reports actual runtime n_ctx
	// (respects --ctx-size), unlike /models which reports n_ctx_train.
	if ctxWindow := detectLlamaCppProps(client, base, apiKey); ctxWindow > 0 {
		return ModelLimits{ContextWindow: ctxWindow}, nil
	}

	// Fall back to /models endpoint for other backends.
	modelsURL := base + "/models"

	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		return ModelLimits{}, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return ModelLimits{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ModelLimits{}, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return ModelLimits{}, err
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
			// llama-server puts context length in meta.n_ctx_train.
			Meta struct {
				NCtxTrain int `json:"n_ctx_train"`
			} `json:"meta"`
			// Some providers (e.g. Ark) return token limits in a nested object.
			TokenLimits *struct {
				ContextWindow       int `json:"context_window"`
				MaxOutputTokenLen   int `json:"max_output_token_length"`
			} `json:"token_limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return ModelLimits{}, err
	}

	for _, m := range result.Data {
		if m.ID == modelID {
			limits := ModelLimits{}
			if m.MaxModelLen > 0 {
				limits.ContextWindow = m.MaxModelLen
			} else if m.Meta.NCtxTrain > 0 {
				limits.ContextWindow = m.Meta.NCtxTrain
			}
			if m.TokenLimits != nil {
				if limits.ContextWindow <= 0 && m.TokenLimits.ContextWindow > 0 {
					limits.ContextWindow = m.TokenLimits.ContextWindow
				}
				if m.TokenLimits.MaxOutputTokenLen > 0 {
					limits.MaxOutput = m.TokenLimits.MaxOutputTokenLen
				}
			}
			return limits, nil
		}
	}

	// If only one model, use it regardless of name match.
	if len(result.Data) == 1 {
		m := result.Data[0]
		limits := ModelLimits{}
		if m.MaxModelLen > 0 {
			limits.ContextWindow = m.MaxModelLen
		} else if m.Meta.NCtxTrain > 0 {
			limits.ContextWindow = m.Meta.NCtxTrain
		}
		if m.TokenLimits != nil {
			if limits.ContextWindow <= 0 && m.TokenLimits.ContextWindow > 0 {
				limits.ContextWindow = m.TokenLimits.ContextWindow
			}
			if m.TokenLimits.MaxOutputTokenLen > 0 {
				limits.MaxOutput = m.TokenLimits.MaxOutputTokenLen
			}
		}
		return limits, nil
	}

	return ModelLimits{}, fmt.Errorf("model %q not found or no limits reported", modelID)
}

// detectLlamaCppProps queries the llama.cpp /props endpoint to get the actual
// runtime context size (n_ctx) which respects --ctx-size configuration.
func detectLlamaCppProps(client *http.Client, base, apiKey string) int {
	// Strip /v1 suffix if present to get the server root.
	propsBase := strings.TrimSuffix(base, "/v1")
	propsURL := propsBase + "/props"

	req, err := http.NewRequest(http.MethodGet, propsURL, nil)
	if err != nil {
		return 0
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0
	}

	var result struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}

	return result.DefaultGenerationSettings.NCtx
}

// lookupBedrockContextWindow returns the advertised max-input context size
// for a Bedrock model ID (or an inference profile that references one).
// Bedrock exposes no API for this — AWS publishes the values in model
// docs — so we keep a small prefix-match table of widely-used models.
//
// The keys are matched by prefix, after stripping the leading region
// qualifier from inference-profile IDs (e.g. "us.anthropic.claude..." →
// "anthropic.claude..."). Returns 0 when unknown; the caller treats 0 as
// "detection unavailable, rely on config override or leave unset".
//
// When AWS adds a model and we haven't updated this table, operators can
// set context_window explicitly in config. The table exists only to avoid
// making them do that for the common cases.
func lookupBedrockContextWindow(modelID string) int {
	// Strip leading "us." / "eu." / "apac." inference-profile qualifier.
	trimmed := modelID
	for _, prefix := range []string{"us.", "eu.", "apac.", "us-gov."} {
		if strings.HasPrefix(trimmed, prefix) {
			trimmed = trimmed[len(prefix):]
			break
		}
	}
	// Longest-prefix-match: map iteration order is random in Go, so iterating
	// until the first hit can pick "cohere.command" (4000) over the longer
	// "cohere.command-r-plus" (128000) for the same model ID. Walk every
	// entry and keep the longest match.
	var bestPrefix string
	var best int
	for prefix, ctx := range bedrockContextWindows {
		if strings.HasPrefix(trimmed, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			best = ctx
		}
	}
	return best
}

// Keyed by Bedrock model-ID prefix (longest match wins — but we iterate
// in insertion order since the map is small and collisions are rare).
// Values are the model's advertised max input context per AWS docs.
var bedrockContextWindows = map[string]int{
	// Anthropic Claude family — 200k unless noted. Claude 3 Opus is older
	// but still 200k.
	"anthropic.claude-3-5-sonnet": 200000,
	"anthropic.claude-3-5-haiku":  200000,
	"anthropic.claude-3-7-sonnet": 200000,
	"anthropic.claude-sonnet-4":   200000,
	"anthropic.claude-opus-4":     200000,
	"anthropic.claude-3-opus":     200000,
	"anthropic.claude-3-sonnet":   200000,
	"anthropic.claude-3-haiku":    200000,
	"anthropic.claude-v2":         100000,
	"anthropic.claude-instant":    100000,

	// Amazon Nova family.
	"amazon.nova-pro":   300000,
	"amazon.nova-lite":  300000,
	"amazon.nova-micro": 128000,

	// Amazon Titan text models.
	"amazon.titan-text-premier": 32000,
	"amazon.titan-text-express": 8000,
	"amazon.titan-text-lite":    4000,

	// Meta Llama family.
	"meta.llama3-70b": 8000,
	"meta.llama3-8b":  8000,
	"meta.llama3-1":   128000,
	"meta.llama3-2":   128000,
	"meta.llama3-3":   128000,
	"meta.llama4":     10000000,

	// Mistral family.
	"mistral.mistral-7b":    32000,
	"mistral.mistral-large": 128000,
	"mistral.mistral-small": 32000,
	"mistral.mixtral-8x7b":  32000,
	"mistral.pixtral-large": 128000,

	// Cohere Command family.
	"cohere.command-r-plus": 128000,
	"cohere.command-r":      128000,
	"cohere.command":        4000,

	// Z.ai GLM family (added to Bedrock in 2026).
	"zai.glm-4.7-flash": 128000,
	"zai.glm-4.6":       128000,
	"zai.glm-4.5":       128000,

	// DeepSeek family.
	"deepseek.r1": 128000,
	"deepseek.v3": 128000,
}

// detectAnthropic queries GET /v1/models/{model_id} on an Anthropic backend
// and extracts max_input_tokens and max_tokens.
func detectAnthropic(client *http.Client, backend, modelID, apiKey string) (ModelLimits, error) {
	base := strings.TrimRight(backend, "/")
	modelURL := base + "/v1/models/" + modelID

	req, err := http.NewRequest(http.MethodGet, modelURL, nil)
	if err != nil {
		return ModelLimits{}, err
	}
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return ModelLimits{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ModelLimits{}, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ModelLimits{}, err
	}

	var result struct {
		MaxInputTokens int `json:"max_input_tokens"`
		MaxTokens      int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return ModelLimits{}, err
	}

	return ModelLimits{
		ContextWindow: result.MaxInputTokens,
		MaxOutput:     result.MaxTokens,
	}, nil
}
