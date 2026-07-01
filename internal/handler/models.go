package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"go-llm-proxy/internal/auth"
	"go-llm-proxy/internal/config"
	"go-llm-proxy/internal/httputil"
)

// ModelsHandler serves GET /v1/models and GET /v1/models/status.
type ModelsHandler struct {
	config *config.ConfigStore
	health *config.HealthStore
}

func NewModelsHandler(cs *config.ConfigStore, health *config.HealthStore) *ModelsHandler {
	return &ModelsHandler{
		config: cs,
		health: health,
	}
}

// ServeHTTP serves GET /v1/models — returns the aggregated model list.
func (h *ModelsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cfg := h.config.Get()
	key := auth.KeyFromContext(r.Context())

	type modelObj struct {
		ID            string `json:"id"`
		Object        string `json:"object"`
		Created       int64  `json:"created"`
		OwnedBy       string `json:"owned_by"`
		ContextWindow int    `json:"context_window,omitempty"`
	}

	models := make([]modelObj, 0, len(cfg.Models))
	seen := make(map[string]bool)
	for _, m := range cfg.Models {
		if !auth.KeyAllowsModel(key, m.Name) {
			continue
		}
		// Only show OpenAI-type models in /v1/models (OpenAI protocol endpoint).
		// Anthropic-type models are accessed via /v1/messages and don't appear here.
		if m.Type == config.BackendAnthropic {
			continue
		}
		if seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		models = append(models, modelObj{
			ID:            m.Name,
			Object:        "model",
			Created:       0,
			OwnedBy:       "organization",
			ContextWindow: m.ContextWindow,
		})
	}

	httputil.SetSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
	}); err != nil {
		slog.Error("failed to write models response", "error", err)
	}
}

// ServeStatus serves GET /v1/models/status — returns health status for all models.
func (h *ModelsHandler) ServeStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.config.Get()
	health := h.health.GetStatus()
	key := auth.KeyFromContext(r.Context())

	type modelStatus struct {
		ID        string `json:"id"`
		Online    bool   `json:"online"`
		LastCheck int64  `json:"last_check"` // unix timestamp, 0 if never checked
		Error     string `json:"error,omitempty"`
	}

	statuses := make([]modelStatus, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		if !auth.KeyAllowsModel(key, m.Name) {
			continue
		}
		if m.Type == config.BackendAnthropic {
			continue
		}

		var s modelStatus
		s.ID = m.Name
		s.Online = true // default to online until first successful check
		s.LastCheck = 0
		s.Error = ""

		if h, ok := health[m.Name]; ok {
			s.Online = h.Online
			s.LastCheck = h.LastCheck.Unix()
			s.Error = h.Error
		}

		statuses = append(statuses, s)
	}

	httputil.SetSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   statuses,
	}); err != nil {
		slog.Error("failed to write model status response", "error", err)
	}
}
