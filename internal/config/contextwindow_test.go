package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDetectOpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "test-model", "max_model_len": 131072},
			},
		})
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	limits, err := detectOpenAI(client, ts.URL+"/v1", "test-model", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limits.ContextWindow != 131072 {
		t.Fatalf("expected context_window 131072, got %d", limits.ContextWindow)
	}
	if limits.MaxOutput != 0 {
		t.Fatalf("expected max_output 0 (not reported), got %d", limits.MaxOutput)
	}
}

func TestDetectOpenAI_WithTokenLimits(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id": "glm-5-2-260617",
					"token_limits": map[string]any{
						"context_window":          1048576,
						"max_output_token_length": 131072,
					},
				},
			},
		})
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	limits, err := detectOpenAI(client, ts.URL+"/v1", "glm-5-2-260617", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limits.ContextWindow != 1048576 {
		t.Fatalf("expected context_window 1048576, got %d", limits.ContextWindow)
	}
	if limits.MaxOutput != 131072 {
		t.Fatalf("expected max_output 131072, got %d", limits.MaxOutput)
	}
}

func TestDetectOpenAI_ModelNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Multiple models — no single-model fallback applies.
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "model-a", "max_model_len": 8192},
				{"id": "model-b", "max_model_len": 16384},
			},
		})
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	_, err := detectOpenAI(client, ts.URL+"/v1", "nonexistent", "")
	if err == nil {
		t.Fatal("expected error for model not found")
	}
}

func TestDetectOpenAI_SingleModelFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "internal-name", "max_model_len": 65536},
			},
		})
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	limits, err := detectOpenAI(client, ts.URL+"/v1", "friendly-name", "")
	if err != nil {
		t.Fatalf("expected single-model fallback, got error: %v", err)
	}
	if limits.ContextWindow != 65536 {
		t.Fatalf("expected context_window 65536, got %d", limits.ContextWindow)
	}
}

func TestDetectAnthropic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":               "claude-test",
			"max_input_tokens": 200000,
			"max_tokens":       8192,
		})
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	limits, err := detectAnthropic(client, ts.URL, "claude-test", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limits.ContextWindow != 200000 {
		t.Fatalf("expected context_window 200000, got %d", limits.ContextWindow)
	}
	if limits.MaxOutput != 8192 {
		t.Fatalf("expected max_output 8192, got %d", limits.MaxOutput)
	}
}

func TestDetectContextWindows_SkipsConfigured(t *testing.T) {
	cfg := &Config{
		Models: []ModelConfig{
			{Name: "test", Backend: "http://localhost:9999/v1", Model: "test", ContextWindow: 99999, MaxOutput: 50000, Timeout: 300},
		},
	}
	cs := &ConfigStore{config: cfg}

	// Should not attempt any network calls (backend is unreachable).
	DetectContextWindows(cs)

	// Values should be unchanged.
	model := cs.Get().Models[0]
	if model.ContextWindow != 99999 {
		t.Fatalf("expected configured context_window preserved, got %d", model.ContextWindow)
	}
	if model.MaxOutput != 50000 {
		t.Fatalf("expected configured max_output preserved, got %d", model.MaxOutput)
	}
}
