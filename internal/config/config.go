package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type ProcessorsConfig struct {
	Vision       string `yaml:"vision"`         // model name for vision processing (empty = disabled)
	Audio        string `yaml:"audio"`          // model name for audio transcription (empty = disabled; pipeline integration pending)
	OCR          string `yaml:"ocr"`            // model name for OCR/text extraction from PDF page images (falls back to vision)
	WebSearchKey string `yaml:"web_search_key"` // web search API key — Tavily or Brave (empty = web search disabled)
}

type Config struct {
	Listen                 string           `yaml:"listen"`
	Models                 []ModelConfig    `yaml:"models"`
	Keys                   []KeyConfig      `yaml:"keys"`
	Providers              []ProviderConfig `yaml:"providers"`              // named provider definitions (shared backend/api_key)
	ModelGroups            []ModelGroupConfig `yaml:"model_groups"`           // virtual model groups for aggregation/failover
	Services               ServicesConfig   `yaml:"services"`               // external service proxies (Qdrant, etc.)
	Processors             ProcessorsConfig `yaml:"processors"`             // global processor defaults
	TrustedProxies         []string         `yaml:"trusted_proxies"`        // CIDR or IPs allowed to set X-Real-IP
	ServeConfigGenerator   bool             `yaml:"serve_config_generator"`   // enable the config generator page at GET /
	LogMetrics             bool             `yaml:"log_metrics"`              // enable per-request usage logging to SQLite
	UsageDB                string           `yaml:"usage_db"`                 // path to SQLite usage database (default: usage.db)
	UsageDashboard         bool             `yaml:"usage_dashboard"`          // enable the usage dashboard at /usage
	UsageDashboardPassword string           `yaml:"usage_dashboard_password"` // password for the usage dashboard
}

// ModelGroupConfig defines a virtual model that aggregates multiple provider
// backends under a single name. The proxy selects a member according to the
// configured strategy (e.g. "sequential") and falls back to the next member
// on failure.
type ModelGroupConfig struct {
	Name           string             `yaml:"name"`     // virtual model name exposed to clients
	Strategy       string             `yaml:"strategy"` // routing strategy: "sequential" (default)
	Members        []ModelGroupMember `yaml:"members"`
	CircuitBreaker *CBSettings        `yaml:"circuit_breaker,omitempty"` // optional breaker tuning
}

// ModelGroupMember defines a single backend within a model group.
// The provider field references a named ProviderConfig; the model field is
// the actual model name sent to that provider's backend.
type ModelGroupMember struct {
	Provider string `yaml:"provider"` // references a ProviderConfig name
	Model    string `yaml:"model"`    // model name sent to the backend
}

// CBSettings configures circuit-breaker behaviour for a model group.
type CBSettings struct {
	FailureThreshold     int `yaml:"failure_threshold"`      // consecutive failures before tripping (default: 3)
	RecoverySeconds      int `yaml:"recovery_seconds"`       // seconds before probing after a regular failure (default: 30)
	QuotaCooldownMinutes int `yaml:"quota_cooldown_minutes"` // minutes before probing after a 429 (default: 60)
}

// ProviderConfig defines a named provider that can be referenced by models
// via the provider field. A model inherits the provider's backend, api_key,
// type, and other connection-level settings, and can override any of them.
type ProviderConfig struct {
	Name   string `yaml:"name"`
	Backend string `yaml:"backend"`
	APIKey string `yaml:"api_key"`
	Type   string `yaml:"type"` // "", "openai" (default), "anthropic", or "bedrock"
	AuthType string `yaml:"auth_type"` // "auto" (default), "bearer", or "x-api-key"
	Status string `yaml:"status"` // "" (default/up) or "down" (manually marked unavailable)

	// AWS Bedrock fields (only used when type: "bedrock").
	Region          string `yaml:"region"`
	AWSAccessKey    string `yaml:"aws_access_key"`
	AWSSecretKey    string `yaml:"aws_secret_key"`
	AWSSessionToken string `yaml:"aws_session_token"`
	GuardrailID      string `yaml:"guardrail_id,omitempty"`
	GuardrailVersion string `yaml:"guardrail_version,omitempty"`
	GuardrailTrace   string `yaml:"guardrail_trace,omitempty"`
}

const (
	BackendOpenAI    = "openai"
	BackendAnthropic = "anthropic"
	BackendBedrock   = "bedrock"

	ResponsesModeAuto      = ""          // default: probe backend, cache result
	ResponsesModeNative    = "native"    // always passthrough
	ResponsesModeTranslate = "translate" // always translate to Chat Completions

	MessagesModeAuto      = ""          // default: anthropic backends passthrough, others translate
	MessagesModeNative    = "native"    // always passthrough (force Anthropic protocol to backend)
	MessagesModeTranslate = "translate" // always translate Anthropic Messages to Chat Completions
)

// SamplingDefaults contains default sampling parameters for a model.
// These are injected into requests that don't specify them.
type SamplingDefaults struct {
	Temperature      *float64 `yaml:"temperature"`       // controls randomness (0.0 = deterministic)
	TopP             *float64 `yaml:"top_p"`             // nucleus sampling threshold
	TopK             *int     `yaml:"top_k"`             // limits vocabulary to top K tokens
	MaxNewTokens     *int     `yaml:"max_new_tokens"`    // maximum tokens to generate (maps to max_tokens)
	FrequencyPenalty *float64 `yaml:"frequency_penalty"` // penalizes repeated tokens by frequency (0.0–2.0)
	PresencePenalty  *float64 `yaml:"presence_penalty"`  // penalizes tokens that have appeared at all (0.0–2.0)
	ReasoningEffort  *string  `yaml:"reasoning_effort"`  // thinking budget: low, medium, or high
	Stop             []string `yaml:"stop"`              // strings that trigger end of generation
}

type ModelConfig struct {
	Name           string            `yaml:"name"`
	Provider       string            `yaml:"provider"`        // reference to a named provider (shares backend/api_key/type)
	Backend        string            `yaml:"backend"`         // upstream URL e.g. http://192.168.100.10:8000/v1 (overrides provider)
	APIKey         string            `yaml:"api_key"`         // key to send to the backend (overrides provider)
	Model          string            `yaml:"model"`           // model name to send to the backend (if different from Name)
	Timeout        int               `yaml:"timeout"`         // request timeout in seconds (default 300)
	Type           string            `yaml:"type"`            // backend type: "" or "openai" (default), "anthropic" (overrides provider)
	AuthType       string            `yaml:"auth_type"`       // "auto" (default), "bearer", or "x-api-key" (overrides provider)
	ResponsesMode  string            `yaml:"responses_mode"`  // "auto" (default), "native", or "translate"
	MessagesMode   string            `yaml:"messages_mode"`   // "auto" (default), "native", or "translate"
	ContextWindow  int               `yaml:"context_window"`  // max context tokens (0 = auto-detect from backend)
	MaxOutput      int               `yaml:"max_output"`      // max output tokens (0 = no clamp); clamps max_tokens/max_completion_tokens before forwarding
	SupportsVision bool              `yaml:"supports_vision"` // model handles images natively
	SupportsAudio  bool              `yaml:"supports_audio"`  // model handles audio (transcription or audio input)
	ForcePipeline  bool              `yaml:"force_pipeline"`  // run pipeline even on native backends
	Processors     *ProcessorsConfig `yaml:"processors"`      // per-model processor overrides (nil = use global)
	Defaults       *SamplingDefaults `yaml:"defaults"`        // default sampling parameters (nil = use backend defaults)

	// AWS Bedrock fields (only used when type: "bedrock").
	// If api_key is set, it is sent as a Bedrock API key bearer token and the
	// SigV4 fields below are ignored. Otherwise SigV4 signing is used with the
	// provided IAM credentials, falling back to AWS_ACCESS_KEY_ID /
	// AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN environment variables.
	Region          string `yaml:"region"`            // AWS region, e.g. "us-east-1"
	AWSAccessKey    string `yaml:"aws_access_key"`    // IAM access key ID (AKIA...)
	AWSSecretKey    string `yaml:"aws_secret_key"`    // IAM secret access key
	AWSSessionToken string `yaml:"aws_session_token"` // optional STS session token

	// Optional Bedrock guardrail configuration. Applied to every request to
	// this model; per-request override is not supported by design. Trace
	// accepts "enabled", "disabled", or "enabled_full" per Bedrock API.
	GuardrailID      string `yaml:"guardrail_id,omitempty"`
	GuardrailVersion string `yaml:"guardrail_version,omitempty"`
	GuardrailTrace   string `yaml:"guardrail_trace,omitempty"`
}

type KeyConfig struct {
	Key    string   `yaml:"key"`
	Name   string   `yaml:"name"`   // friendly name for logging
	Models []string `yaml:"models"` // allowed models, empty = all
}

// ServicesConfig contains configuration for external services proxied by the server.
type ServicesConfig struct {
	Qdrant *QdrantConfig `yaml:"qdrant"`
}

// QdrantConfig configures the Qdrant vector database proxy.
type QdrantConfig struct {
	Backend string         `yaml:"backend"` // Qdrant server URL e.g. http://192.168.5.143:6333
	APIKey  string         `yaml:"api_key"` // API key to send to Qdrant backend
	AppKeys []AppKeyConfig `yaml:"app_keys"`
}

// AppKeyConfig defines an application key for service access.
type AppKeyConfig struct {
	Name string `yaml:"name"` // friendly name for logging
	Key  string `yaml:"key"`  // the actual API key
}

// ConfigStore provides thread-safe access to the current config.
type ConfigStore struct {
	mu       sync.RWMutex
	writeMu  sync.Mutex // serializes in-process writes to the config file
	config   *Config
	path     string
	onReload func(*Config) // called after each successful reload (optional)
}

func NewConfigStore(path string) (*ConfigStore, error) {
	cs := &ConfigStore{path: path}
	if err := cs.Load(); err != nil {
		return nil, err
	}
	return cs, nil
}

// NewTestConfigStore creates a ConfigStore from an in-memory Config (for testing).
func NewTestConfigStore(cfg *Config) *ConfigStore {
	return &ConfigStore{config: cfg}
}

func (cs *ConfigStore) Load() error {
	data, err := os.ReadFile(cs.path)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}

	for i := range cfg.Models {
		m := &cfg.Models[i]
		if m.Timeout == 0 {
			m.Timeout = 300
		}
		if m.Model == "" {
			m.Model = m.Name
		}

		// Resolve provider reference: inherit backend/api_key/type from the
		// named provider, and Bedrock-specific fields when applicable.
		if m.Provider != "" {
			var p *ProviderConfig
			for j := range cfg.Providers {
				if cfg.Providers[j].Name == m.Provider {
					p = &cfg.Providers[j]
					break
				}
			}
			if p == nil {
				return fmt.Errorf("model %q references unknown provider %q", m.Name, m.Provider)
			}
			if m.Backend == "" {
				m.Backend = p.Backend
			}
			if m.APIKey == "" {
				m.APIKey = p.APIKey
			}
			if m.Type == "" {
				m.Type = p.Type
			}
			if m.AuthType == "" {
				m.AuthType = p.AuthType
			}
			// Inherit Bedrock fields from provider when model doesn't set them.
			if m.Region == "" {
				m.Region = p.Region
			}
			if m.AWSAccessKey == "" {
				m.AWSAccessKey = p.AWSAccessKey
			}
			if m.AWSSecretKey == "" {
				m.AWSSecretKey = p.AWSSecretKey
			}
			if m.AWSSessionToken == "" {
				m.AWSSessionToken = p.AWSSessionToken
			}
		}

		if m.Type == BackendBedrock {
			applyBedrockDefaults(m)
		}
	}

	if err := validateConfig(&cfg); err != nil {
		return err
	}

	cs.mu.Lock()
	cs.config = &cfg
	cs.mu.Unlock()

	slog.Info("config loaded", "models", len(cfg.Models), "keys", len(cfg.Keys))

	if cs.onReload != nil {
		cs.onReload(&cfg)
	}
	return nil
}

// SetOnReload registers a callback invoked after each successful config reload.
func (cs *ConfigStore) SetOnReload(fn func(*Config)) {
	cs.onReload = fn
}

func (cs *ConfigStore) Get() *Config {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config
}

// Watch starts a goroutine that reloads config when the file changes on disk.
// Watches the parent directory to survive rename-based saves (vim, etc.).
// Returns a stop function.
func (cs *ConfigStore) Watch() (stop func(), err error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating file watcher: %w", err)
	}

	absPath, err := filepath.Abs(cs.path)
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("resolving config path: %w", err)
	}
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watching %s: %w", dir, err)
	}

	go func() {
		// Debounce: editors often write a temp file then rename, producing
		// multiple events in quick succession. Wait briefly before reloading.
		var debounce *time.Timer
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Only react to events on our config file.
				if filepath.Base(event.Name) != base {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(500*time.Millisecond, func() {
					slog.Info("config file changed, reloading")
					if err := cs.Load(); err != nil {
						slog.Error("failed to reload config", "error", err)
					}
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("file watcher error", "error", err)
			}
		}
	}()

	slog.Info("watching config file for changes", "path", absPath)
	return func() { watcher.Close() }, nil
}

// applyBedrockDefaults fills in the default Bedrock backend URL and pulls AWS
// credentials from the environment when not explicitly configured. Called for
// every model with type: "bedrock" during config load.
func applyBedrockDefaults(m *ModelConfig) {
	if m.Backend == "" && m.Region != "" {
		m.Backend = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", m.Region)
	}
	// API-key auth shortcuts SigV4 entirely; only fall back to env for IAM keys.
	if m.APIKey != "" {
		return
	}
	if m.AWSAccessKey == "" {
		m.AWSAccessKey = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if m.AWSSecretKey == "" {
		m.AWSSecretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if m.AWSSessionToken == "" {
		m.AWSSessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}
}

// FindModel returns the ModelConfig matching the given name and optional type hint.
// When typeHint is non-empty, it first tries an exact (name, type) match, then
// falls back to name-only lookup. This allows models with the same name but
// different types (e.g. "ds-flash" for both openai and anthropic backends).
func FindModel(cfg *Config, name string, typeHint ...string) *ModelConfig {
	if len(typeHint) > 0 && typeHint[0] != "" {
		for i := range cfg.Models {
			if cfg.Models[i].Name == name && cfg.Models[i].Type == typeHint[0] {
				return &cfg.Models[i]
			}
		}
	}
	for i := range cfg.Models {
		if cfg.Models[i].Name == name {
			return &cfg.Models[i]
		}
	}
	return nil
}

// FindModelGroup returns the ModelGroupConfig matching the given name, or nil.
func FindModelGroup(cfg *Config, name string) *ModelGroupConfig {
	for i := range cfg.ModelGroups {
		if cfg.ModelGroups[i].Name == name {
			return &cfg.ModelGroups[i]
		}
	}
	return nil
}

func validateConfig(cfg *Config) error {
	if len(cfg.Keys) == 0 {
		slog.Warn("no API keys configured — all requests will be unauthenticated")
	}

	if cfg.UsageDashboard {
		if !cfg.LogMetrics {
			return fmt.Errorf("usage_dashboard requires log_metrics to be enabled")
		}
		if cfg.UsageDashboardPassword == "" {
			return fmt.Errorf("usage_dashboard requires usage_dashboard_password to be set")
		}
	}

	seen := make(map[string]bool) // "name:type" uniqueness
	for _, m := range cfg.Models {
		if m.Name == "" {
			return fmt.Errorf("model entry missing name")
		}
		if m.Backend == "" {
			return fmt.Errorf("model %q missing backend", m.Name)
		}

		// Validate backend URL.
		u, err := url.Parse(m.Backend)
		if err != nil {
			return fmt.Errorf("model %q has invalid backend URL: %w", m.Name, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("model %q backend must use http or https scheme, got %q", m.Name, u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("model %q backend missing host", m.Name)
		}
		if u.User != nil {
			return fmt.Errorf("model %q backend must not contain credentials in URL", m.Name)
		}

		switch m.Type {
		case "", BackendOpenAI, BackendAnthropic:
		case BackendBedrock:
			if m.Region == "" {
				return fmt.Errorf("model %q (bedrock) requires region", m.Name)
			}
			if m.APIKey == "" && (m.AWSAccessKey == "" || m.AWSSecretKey == "") {
				return fmt.Errorf("model %q (bedrock) requires either api_key (Bedrock API key) or aws_access_key + aws_secret_key (set in config or environment)", m.Name)
			}
			// Soft check: cross-region inference profile IDs are prefixed with
			// a region scope (us., eu., apac., us-gov.). When the prefix and
			// the configured region obviously disagree, warn at startup —
			// the request would otherwise fail opaquely at the first call.
			// Not a hard error: AWS periodically adds new scope prefixes and
			// we don't want to gate startup on our allowlist keeping up.
			warnIfCrossRegionProfileMismatch(m.Name, m.Model, m.Region)
			if m.MessagesMode != "" && m.MessagesMode != "auto" {
				slog.Warn("messages_mode has no effect on bedrock backends (always translates to Converse)",
					"model", m.Name, "messages_mode", m.MessagesMode)
			}
			if m.ResponsesMode != "" && m.ResponsesMode != "auto" {
				slog.Warn("responses_mode has no effect on bedrock backends (Responses API is not supported)",
					"model", m.Name, "responses_mode", m.ResponsesMode)
			}
		default:
			return fmt.Errorf("model %q has unknown type %q (must be %q, %q, or %q)", m.Name, m.Type, BackendOpenAI, BackendAnthropic, BackendBedrock)
		}

		switch m.ResponsesMode {
		case "", "auto", ResponsesModeNative, ResponsesModeTranslate:
		default:
			return fmt.Errorf("model %q has unknown responses_mode %q (must be %q, %q, or omitted)", m.Name, m.ResponsesMode, ResponsesModeNative, ResponsesModeTranslate)
		}

		switch m.MessagesMode {
		case "", "auto", MessagesModeNative, MessagesModeTranslate:
		default:
			return fmt.Errorf("model %q has unknown messages_mode %q (must be %q, %q, or omitted)", m.Name, m.MessagesMode, MessagesModeNative, MessagesModeTranslate)
		}

		if d := m.Defaults; d != nil && d.ReasoningEffort != nil {
			switch *d.ReasoningEffort {
			case "low", "medium", "high":
			default:
				return fmt.Errorf("model %q has unknown reasoning_effort %q (must be low, medium, or high)", m.Name, *d.ReasoningEffort)
			}
		}

		// Warn when max_output is unset but context_window is configured.
		// Without max_output, the proxy won't clamp the client's max_tokens,
		// which can cause backend 400 errors if the client sends a value
		// exceeding the backend's output limit.
		if m.MaxOutput <= 0 && m.ContextWindow > 0 {
			slog.Warn("model has no max_output limit — client max_tokens will not be clamped, "+
				"which may cause backend 400 errors if the client sends a value exceeding the backend's output limit",
				"model", m.Name, "context_window", m.ContextWindow)
		}

		key := m.Name + ":" + m.Type
		if seen[key] {
			return fmt.Errorf("duplicate model %q with type %q", m.Name, m.Type)
		}
		seen[key] = true
	}

	// Validate model groups.
	groupNames := make(map[string]bool)
	for _, g := range cfg.ModelGroups {
		if g.Name == "" {
			return fmt.Errorf("model_group entry missing name")
		}
		if groupNames[g.Name] {
			return fmt.Errorf("duplicate model_group %q", g.Name)
		}
		groupNames[g.Name] = true

		// Group name must not conflict with a model name.
		if seen[g.Name+":"] {
			return fmt.Errorf("model_group %q conflicts with an existing model name", g.Name)
		}

		switch g.Strategy {
		case "", "sequential":
		default:
			return fmt.Errorf("model_group %q has unknown strategy %q (must be %q)", g.Name, g.Strategy, "sequential")
		}

		if len(g.Members) == 0 {
			return fmt.Errorf("model_group %q has no members", g.Name)
		}

		for i, member := range g.Members {
			if member.Provider == "" {
				return fmt.Errorf("model_group %q member %d missing provider", g.Name, i)
			}
			if member.Model == "" {
				return fmt.Errorf("model_group %q member %d missing model", g.Name, i)
			}
			// Verify the provider exists.
			found := false
			for _, p := range cfg.Providers {
				if p.Name == member.Provider {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("model_group %q member %d references unknown provider %q", g.Name, i, member.Provider)
			}
		}
	}

	// Validate provider status values.
	for _, p := range cfg.Providers {
		switch p.Status {
		case "", "down":
		default:
			return fmt.Errorf("provider %q has unknown status %q (must be %q or %q)", p.Name, p.Status, "", "down")
		}
	}

	// Build a name-only set for processor reference checks (processors reference
	// models by name, not by name+type). Also includes group names so that
	// key permissions can reference virtual model names.
	allNames := make(map[string]bool)
	for _, m := range cfg.Models {
		allNames[m.Name] = true
	}
	for _, g := range cfg.ModelGroups {
		allNames[g.Name] = true
	}

	// Validate global vision processor references a defined model.
	if v := cfg.Processors.Vision; v != "" {
		if !allNames[v] {
			return fmt.Errorf("global processors.vision references unknown model %q", v)
		}
	}

	// Validate global OCR processor references a defined model.
	if v := cfg.Processors.OCR; v != "" && v != "none" {
		if !allNames[v] {
			return fmt.Errorf("global processors.ocr references unknown model %q", v)
		}
	}

	// Validate global audio processor references a defined model.
	if v := cfg.Processors.Audio; v != "" && v != "none" {
		if !allNames[v] {
			return fmt.Errorf("global processors.audio references unknown model %q", v)
		}
	}

	// Validate per-model processor overrides reference defined models.
	for _, m := range cfg.Models {
		if m.Processors != nil && m.Processors.Vision != "" && m.Processors.Vision != "none" {
			if !allNames[m.Processors.Vision] {
				return fmt.Errorf("model %q processors.vision references unknown model %q", m.Name, m.Processors.Vision)
			}
		}
		if m.Processors != nil && m.Processors.OCR != "" && m.Processors.OCR != "none" {
			if !allNames[m.Processors.OCR] {
				return fmt.Errorf("model %q processors.ocr references unknown model %q", m.Name, m.Processors.OCR)
			}
		}
	}

	// Auto-infer SupportsVision: any model referenced as a vision processor
	// obviously supports vision — don't require the user to say so twice.
	visionModels := make(map[string]bool)
	if cfg.Processors.Vision != "" {
		visionModels[cfg.Processors.Vision] = true
	}
	for _, m := range cfg.Models {
		if m.Processors != nil && m.Processors.Vision != "" && m.Processors.Vision != "none" {
			visionModels[m.Processors.Vision] = true
		}
	}
	for i := range cfg.Models {
		if visionModels[cfg.Models[i].Name] && !cfg.Models[i].SupportsVision {
			cfg.Models[i].SupportsVision = true
		}
	}

	// Auto-infer SupportsAudio: any model referenced as the global audio
	// processor obviously handles audio — don't require it to be set twice.
	if cfg.Processors.Audio != "" && cfg.Processors.Audio != "none" {
		for i := range cfg.Models {
			if cfg.Models[i].Name == cfg.Processors.Audio && !cfg.Models[i].SupportsAudio {
				cfg.Models[i].SupportsAudio = true
			}
		}
	}

	keys := make(map[string]bool)
	for _, k := range cfg.Keys {
		if k.Key == "" {
			return fmt.Errorf("key entry missing key value")
		}
		if keys[k.Key] {
			return fmt.Errorf("duplicate key for %q", k.Name)
		}
		keys[k.Key] = true

		for _, m := range k.Models {
			if !allNames[m] {
				return fmt.Errorf("key %q references unknown model %q", k.Name, m)
			}
		}
	}

	// Validate Qdrant service config.
	if q := cfg.Services.Qdrant; q != nil {
		if q.Backend == "" {
			return fmt.Errorf("services.qdrant missing backend")
		}
		u, err := url.Parse(q.Backend)
		if err != nil {
			return fmt.Errorf("services.qdrant has invalid backend URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("services.qdrant backend must use http or https scheme, got %q", u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("services.qdrant backend missing host")
		}

		appKeys := make(map[string]bool)
		for _, ak := range q.AppKeys {
			if ak.Key == "" {
				return fmt.Errorf("services.qdrant app_key entry missing key value")
			}
			if ak.Name == "" {
				return fmt.Errorf("services.qdrant app_key entry missing name")
			}
			if appKeys[ak.Key] {
				return fmt.Errorf("services.qdrant duplicate app_key for %q", ak.Name)
			}
			appKeys[ak.Key] = true
		}
	}

	return nil
}

// ApplySamplingDefaults injects default sampling parameters into a Chat Completions
// request map. Only sets values that are not already present in the request.
// This allows per-model defaults to be applied without overriding explicit user values.
func (m *ModelConfig) ApplySamplingDefaults(chatReq map[string]any) {
	if m.Defaults == nil {
		return
	}
	d := m.Defaults
	var applied []string

	if d.Temperature != nil {
		if _, exists := chatReq["temperature"]; !exists {
			chatReq["temperature"] = *d.Temperature
			applied = append(applied, fmt.Sprintf("temperature=%.2f", *d.Temperature))
		}
	}
	if d.TopP != nil {
		if _, exists := chatReq["top_p"]; !exists {
			chatReq["top_p"] = *d.TopP
			applied = append(applied, fmt.Sprintf("top_p=%.2f", *d.TopP))
		}
	}
	if d.TopK != nil {
		if _, exists := chatReq["top_k"]; !exists {
			chatReq["top_k"] = *d.TopK
			applied = append(applied, fmt.Sprintf("top_k=%d", *d.TopK))
		}
	}
	if d.MaxNewTokens != nil {
		if _, exists := chatReq["max_tokens"]; !exists {
			chatReq["max_tokens"] = *d.MaxNewTokens
			applied = append(applied, fmt.Sprintf("max_tokens=%d", *d.MaxNewTokens))
		}
	}
	if d.FrequencyPenalty != nil {
		if _, exists := chatReq["frequency_penalty"]; !exists {
			chatReq["frequency_penalty"] = *d.FrequencyPenalty
			applied = append(applied, fmt.Sprintf("frequency_penalty=%.2f", *d.FrequencyPenalty))
		}
	}
	if d.PresencePenalty != nil {
		if _, exists := chatReq["presence_penalty"]; !exists {
			chatReq["presence_penalty"] = *d.PresencePenalty
			applied = append(applied, fmt.Sprintf("presence_penalty=%.2f", *d.PresencePenalty))
		}
	}
	if d.ReasoningEffort != nil {
		if _, exists := chatReq["reasoning_effort"]; !exists {
			chatReq["reasoning_effort"] = *d.ReasoningEffort
			applied = append(applied, fmt.Sprintf("reasoning_effort=%s", *d.ReasoningEffort))
		}
	}
	if len(d.Stop) > 0 {
		if _, exists := chatReq["stop"]; !exists {
			chatReq["stop"] = d.Stop
			applied = append(applied, fmt.Sprintf("stop=%v", d.Stop))
		}
	}

	if len(applied) > 0 {
		slog.Debug("applied sampling defaults", "model", m.Name, "params", applied)
	}
}

// ClampMaxTokens clamps max_tokens / max_completion_tokens in a Chat Completions
// request body to the model's MaxOutput limit. If MaxOutput is 0 (unset), no
// clamping is performed. This prevents the client from requesting more output
// tokens than the backend supports (e.g. Ollama Cloud deepseek-v4-flash caps at
// 65536 output tokens).
func (m *ModelConfig) ClampMaxTokens(chatReq map[string]any) {
	if m.MaxOutput <= 0 {
		return
	}
	// max_completion_tokens (newer field) takes precedence over max_tokens.
	for _, key := range []string{"max_completion_tokens", "max_tokens"} {
		if val, exists := chatReq[key]; exists {
			switch v := val.(type) {
			case float64:
				if int(v) > m.MaxOutput {
					chatReq[key] = float64(m.MaxOutput)
					slog.Debug("clamped max_tokens", "model", m.Name, "key", key,
						"from", int(v), "to", m.MaxOutput)
				}
			case int:
				if v > m.MaxOutput {
					chatReq[key] = m.MaxOutput
					slog.Debug("clamped max_tokens", "model", m.Name, "key", key,
						"from", v, "to", m.MaxOutput)
				}
			}
		}
	}
}

// warnIfCrossRegionProfileMismatch emits a startup warning when a Bedrock
// cross-region inference profile ID carries a region-scope prefix that
// obviously disagrees with the configured region.
//
// Example: model "eu.anthropic.claude-3-5-sonnet-20241022-v2:0" with region
// "us-east-1" will fail at first request because the EU profile is only
// served from EU regions. Catching this at startup turns an opaque 400 into
// an actionable warning.
//
// Deliberately NOT a hard error: AWS adds new scope prefixes periodically
// (apac., us-gov., future mc-...) and a strict allowlist would make
// upgrades painful.
func warnIfCrossRegionProfileMismatch(modelName, modelID, region string) {
	// Known region-scope prefixes as of 2026-04. Extend as new ones appear.
	scopes := map[string]string{
		"us.":     "us-",
		"eu.":     "eu-",
		"apac.":   "ap-",
		"us-gov.": "us-gov-",
	}
	for prefix, regionStub := range scopes {
		if !stringHasPrefix(modelID, prefix) {
			continue
		}
		if !stringHasPrefix(region, regionStub) {
			slog.Warn("bedrock inference profile region prefix may not match configured region",
				"model", modelName, "model_id", modelID, "region", region,
				"expected_region_prefix", regionStub)
		}
		return
	}
}

// stringHasPrefix is a tiny inlineable helper — pulled out so the warning
// function above stays readable without pulling in strings just for HasPrefix.
func stringHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
