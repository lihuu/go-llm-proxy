package config

import (
	"strings"
	"testing"
)

func validConfig() *Config {
	return &Config{
		Listen: ":8080",
		Models: []ModelConfig{
			{Name: "test-model", Backend: "http://localhost:8000/v1", Model: "test-model", Timeout: 300},
		},
		Keys: []KeyConfig{
			{Key: "sk-test-key", Name: "admin"},
		},
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	cfg := validConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_NoKeys(t *testing.T) {
	cfg := validConfig()
	cfg.Keys = nil
	// No keys is a warning, not an error.
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for empty keys, got: %v", err)
	}
}

func TestValidateConfig_MissingModelName(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Name = ""
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing name") {
		t.Fatalf("expected missing name error, got: %v", err)
	}
}

func TestValidateConfig_MissingBackend(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Backend = ""
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing backend") {
		t.Fatalf("expected missing backend error, got: %v", err)
	}
}

func TestValidateConfig_InvalidScheme(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Backend = "ftp://localhost:8000/v1"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected scheme error, got: %v", err)
	}
}

func TestValidateConfig_BackendWithCredentials(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Backend = "http://user:pass@localhost:8000/v1"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentials error, got: %v", err)
	}
}

func TestValidateConfig_DuplicateModelName(t *testing.T) {
	cfg := validConfig()
	cfg.Models = append(cfg.Models, ModelConfig{
		Name: "test-model", Backend: "http://localhost:8001/v1", Model: "test-model", Timeout: 300,
	})
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate model") {
		t.Fatalf("expected duplicate model error, got: %v", err)
	}
}

func TestValidateConfig_DuplicateKey(t *testing.T) {
	cfg := validConfig()
	cfg.Keys = append(cfg.Keys, KeyConfig{Key: "sk-test-key", Name: "dup"})
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate key error, got: %v", err)
	}
}

func TestValidateConfig_KeyReferencesUnknownModel(t *testing.T) {
	cfg := validConfig()
	cfg.Keys[0].Models = []string{"nonexistent"}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("expected unknown model error, got: %v", err)
	}
}

func TestValidateConfig_MissingKeyValue(t *testing.T) {
	cfg := validConfig()
	cfg.Keys[0].Key = ""
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing key value") {
		t.Fatalf("expected missing key error, got: %v", err)
	}
}

func TestValidateConfig_MissingHost(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Backend = "http:///v1"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing host") {
		t.Fatalf("expected missing host error, got: %v", err)
	}
}

func TestValidateConfig_ValidAnthropicType(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendAnthropic
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for anthropic type, got: %v", err)
	}
}

func TestValidateConfig_ValidOpenAIType(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendOpenAI
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for openai type, got: %v", err)
	}
}

func TestValidateConfig_UnknownType(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = "gemini"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type error, got: %v", err)
	}
}

func TestValidateConfig_ValidBedrockTypeWithIAMKeys(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendBedrock
	cfg.Models[0].Backend = "https://bedrock-runtime.us-east-1.amazonaws.com"
	cfg.Models[0].Region = "us-east-1"
	cfg.Models[0].AWSAccessKey = "AKIAEXAMPLE"
	cfg.Models[0].AWSSecretKey = "secret"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for bedrock type with IAM keys, got: %v", err)
	}
}

func TestValidateConfig_ValidBedrockTypeWithAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendBedrock
	cfg.Models[0].Backend = "https://bedrock-runtime.us-east-1.amazonaws.com"
	cfg.Models[0].Region = "us-east-1"
	cfg.Models[0].APIKey = "bdrk-key"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for bedrock type with API key, got: %v", err)
	}
}

func TestValidateConfig_BedrockMissingRegion(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendBedrock
	cfg.Models[0].Backend = "https://bedrock-runtime.us-east-1.amazonaws.com"
	cfg.Models[0].APIKey = "bdrk-key"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("expected region required error, got: %v", err)
	}
}

func TestValidateConfig_BedrockMissingCredentials(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendBedrock
	cfg.Models[0].Backend = "https://bedrock-runtime.us-east-1.amazonaws.com"
	cfg.Models[0].Region = "us-east-1"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("expected credentials required error, got: %v", err)
	}
}

func TestValidateConfig_BedrockMissingSecretKey(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Type = BackendBedrock
	cfg.Models[0].Backend = "https://bedrock-runtime.us-east-1.amazonaws.com"
	cfg.Models[0].Region = "us-east-1"
	cfg.Models[0].AWSAccessKey = "AKIAEXAMPLE"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "aws_secret_key") {
		t.Fatalf("expected secret key required error, got: %v", err)
	}
}

func TestValidateConfig_ValidMessagesMode(t *testing.T) {
	for _, mode := range []string{"", "auto", "native", "translate"} {
		cfg := validConfig()
		cfg.Models[0].MessagesMode = mode
		if err := validateConfig(cfg); err != nil {
			t.Fatalf("expected no error for messages_mode %q, got: %v", mode, err)
		}
	}
}

func TestValidateConfig_UnknownMessagesMode(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].MessagesMode = "bogus"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown messages_mode") {
		t.Fatalf("expected unknown messages_mode error, got: %v", err)
	}
}

func TestValidateConfig_DashboardRequiresMetrics(t *testing.T) {
	cfg := validConfig()
	cfg.UsageDashboard = true
	cfg.UsageDashboardPassword = "secret"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "log_metrics") {
		t.Fatalf("expected log_metrics requirement error, got: %v", err)
	}
}

func TestValidateConfig_DashboardRequiresPassword(t *testing.T) {
	cfg := validConfig()
	cfg.LogMetrics = true
	cfg.UsageDashboard = true
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "usage_dashboard_password") {
		t.Fatalf("expected password requirement error, got: %v", err)
	}
}

func TestValidateConfig_DashboardValid(t *testing.T) {
	cfg := validConfig()
	cfg.LogMetrics = true
	cfg.UsageDashboard = true
	cfg.UsageDashboardPassword = "a-strong-password"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for valid dashboard config, got: %v", err)
	}
}

func TestValidateConfig_DashboardDisabledNoValidation(t *testing.T) {
	cfg := validConfig()
	cfg.UsageDashboard = false
	cfg.UsageDashboardPassword = ""
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error when dashboard disabled, got: %v", err)
	}
}

func TestValidateConfig_GlobalVisionProcessorValid(t *testing.T) {
	cfg := validConfig()
	cfg.Processors.Vision = "test-model"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_GlobalVisionProcessorUnknownModel(t *testing.T) {
	cfg := validConfig()
	cfg.Processors.Vision = "nonexistent"
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "processors.vision references unknown model") {
		t.Fatalf("expected unknown model error, got: %v", err)
	}
}

func TestValidateConfig_PerModelVisionProcessorValid(t *testing.T) {
	cfg := validConfig()
	cfg.Models = append(cfg.Models, ModelConfig{
		Name: "vision-model", Backend: "http://localhost:8001/v1", Model: "vision-model", Timeout: 300,
	})
	cfg.Models[0].Processors = &ProcessorsConfig{Vision: "vision-model"}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_PerModelVisionProcessorNone(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Processors = &ProcessorsConfig{Vision: "none"}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for vision=none, got: %v", err)
	}
}

func TestValidateConfig_PerModelVisionProcessorUnknown(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].Processors = &ProcessorsConfig{Vision: "nonexistent"}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "processors.vision references unknown model") {
		t.Fatalf("expected unknown model error, got: %v", err)
	}
}

func TestValidateConfig_SupportsVisionField(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].SupportsVision = true
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for supports_vision, got: %v", err)
	}
}

func TestValidateConfig_ForcePipelineField(t *testing.T) {
	cfg := validConfig()
	cfg.Models[0].ForcePipeline = true
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for force_pipeline, got: %v", err)
	}
}

func TestApplySamplingDefaults(t *testing.T) {
	temp := 0.9
	topP := 0.95
	topK := 50
	maxTokens := 1024
	model := &ModelConfig{
		Name:    "test",
		Backend: "http://localhost:8000/v1",
		Defaults: &SamplingDefaults{
			Temperature:  &temp,
			TopP:         &topP,
			TopK:         &topK,
			MaxNewTokens: &maxTokens,
			Stop:         []string{"END", "STOP"},
		},
	}

	// Test that defaults are applied to empty request.
	req := map[string]any{"model": "test"}
	model.ApplySamplingDefaults(req)

	if req["temperature"] != temp {
		t.Errorf("expected temperature %v, got %v", temp, req["temperature"])
	}
	if req["top_p"] != topP {
		t.Errorf("expected top_p %v, got %v", topP, req["top_p"])
	}
	if req["top_k"] != topK {
		t.Errorf("expected top_k %v, got %v", topK, req["top_k"])
	}
	if req["max_tokens"] != maxTokens {
		t.Errorf("expected max_tokens %v, got %v", maxTokens, req["max_tokens"])
	}
	stop, ok := req["stop"].([]string)
	if !ok || len(stop) != 2 {
		t.Errorf("expected stop [END STOP], got %v", req["stop"])
	}

	// Test that existing values are not overwritten.
	req2 := map[string]any{
		"model":       "test",
		"temperature": 0.5,
		"max_tokens":  500,
	}
	model.ApplySamplingDefaults(req2)

	if req2["temperature"] != 0.5 {
		t.Errorf("temperature should not be overwritten, got %v", req2["temperature"])
	}
	if req2["max_tokens"] != 500 {
		t.Errorf("max_tokens should not be overwritten, got %v", req2["max_tokens"])
	}
	// But other defaults should be applied.
	if req2["top_p"] != topP {
		t.Errorf("expected top_p %v, got %v", topP, req2["top_p"])
	}
}

func TestApplySamplingDefaults_NilDefaults(t *testing.T) {
	model := &ModelConfig{
		Name:     "test",
		Backend:  "http://localhost:8000/v1",
		Defaults: nil,
	}

	req := map[string]any{"model": "test"}
	model.ApplySamplingDefaults(req)

	// Should not add any fields.
	if _, exists := req["temperature"]; exists {
		t.Error("should not add temperature when defaults is nil")
	}
}

func TestClampMaxTokens_NoOpWhenUnset(t *testing.T) {
	model := &ModelConfig{Name: "test", MaxOutput: 0}
	req := map[string]any{"max_tokens": 100000}
	model.ClampMaxTokens(req)
	if req["max_tokens"] != 100000 {
		t.Fatalf("expected no change, got %v", req["max_tokens"])
	}
}

func TestClampMaxTokens_ClampsMaxTokens(t *testing.T) {
	model := &ModelConfig{Name: "test", MaxOutput: 65536}
	req := map[string]any{"max_tokens": float64(384000)}
	model.ClampMaxTokens(req)
	got, ok := req["max_tokens"].(float64)
	if !ok || int(got) != 65536 {
		t.Fatalf("expected 65536, got %v (type %T)", req["max_tokens"], req["max_tokens"])
	}
}

func TestClampMaxTokens_ClampsMaxCompletionTokens(t *testing.T) {
	model := &ModelConfig{Name: "test", MaxOutput: 65536}
	req := map[string]any{"max_completion_tokens": float64(384000)}
	model.ClampMaxTokens(req)
	got, ok := req["max_completion_tokens"].(float64)
	if !ok || int(got) != 65536 {
		t.Fatalf("expected 65536, got %v (type %T)", req["max_completion_tokens"], req["max_completion_tokens"])
	}
}

func TestClampMaxTokens_MaxCompletionTokensWins(t *testing.T) {
	model := &ModelConfig{Name: "test", MaxOutput: 65536}
	req := map[string]any{"max_completion_tokens": float64(384000), "max_tokens": 1000}
	model.ClampMaxTokens(req)
	got, ok := req["max_completion_tokens"].(float64)
	if !ok || int(got) != 65536 {
		t.Fatalf("expected max_completion_tokens=65536, got %v (type %T)", req["max_completion_tokens"], req["max_completion_tokens"])
	}
	if req["max_tokens"] != 1000 {
		t.Fatalf("expected max_tokens=1000, got %v", req["max_tokens"])
	}
}

func TestClampMaxTokens_NoClampWhenBelowLimit(t *testing.T) {
	model := &ModelConfig{Name: "test", MaxOutput: 65536}
	req := map[string]any{"max_tokens": 32000}
	model.ClampMaxTokens(req)
	if req["max_tokens"] != 32000 {
		t.Fatalf("expected 32000 unchanged, got %v", req["max_tokens"])
	}
}

// --- Model group validation tests ---

func TestValidateConfig_ModelGroupValid(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{
		{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
		{Name: "p2", Backend: "http://p2/v1", Type: "openai"},
	}
	cfg.ModelGroups = []ModelGroupConfig{
		{
			Name:     "my-group",
			Strategy: "sequential",
			Members: []ModelGroupMember{
				{Provider: "p1", Model: "model-a"},
				{Provider: "p2", Model: "model-b"},
			},
		},
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupMissingName(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Backend: "http://p1/v1"}}
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "", Members: []ModelGroupMember{{Provider: "p1", Model: "m"}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing name") {
		t.Fatalf("expected missing name error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupDuplicateName(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Backend: "http://p1/v1"}}
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "dup", Members: []ModelGroupMember{{Provider: "p1", Model: "m"}}},
		{Name: "dup", Members: []ModelGroupMember{{Provider: "p1", Model: "m"}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate model_group") {
		t.Fatalf("expected duplicate model_group error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupConflictsWithModel(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Backend: "http://p1/v1"}}
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "test-model", Members: []ModelGroupMember{{Provider: "p1", Model: "m"}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "conflicts with an existing model") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupUnknownStrategy(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Backend: "http://p1/v1"}}
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "g", Strategy: "round-robin", Members: []ModelGroupMember{{Provider: "p1", Model: "m"}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown strategy") {
		t.Fatalf("expected unknown strategy error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupNoMembers(t *testing.T) {
	cfg := validConfig()
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "g", Members: nil},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "no members") {
		t.Fatalf("expected no members error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupMissingProvider(t *testing.T) {
	cfg := validConfig()
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "g", Members: []ModelGroupMember{{Provider: "", Model: "m"}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing provider") {
		t.Fatalf("expected missing provider error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupMissingModel(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Backend: "http://p1/v1"}}
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "g", Members: []ModelGroupMember{{Provider: "p1", Model: ""}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing model") {
		t.Fatalf("expected missing model error, got: %v", err)
	}
}

func TestValidateConfig_ModelGroupUnknownProvider(t *testing.T) {
	cfg := validConfig()
	cfg.ModelGroups = []ModelGroupConfig{
		{Name: "g", Members: []ModelGroupMember{{Provider: "nonexistent", Model: "m"}}},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got: %v", err)
	}
}

// --- Provider status validation tests ---

func TestValidateConfig_ProviderStatusDown(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{
		{Name: "p1", Backend: "http://p1/v1", Status: "down"},
	}
	// Provider status "down" is valid.
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for status=down, got: %v", err)
	}
}

func TestValidateConfig_ProviderStatusInvalid(t *testing.T) {
	cfg := validConfig()
	cfg.Providers = []ProviderConfig{
		{Name: "p1", Backend: "http://p1/v1", Status: "bogus"},
	}
	err := validateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Fatalf("expected unknown status error, got: %v", err)
	}
}

// --- FindModelGroup tests ---

func TestFindModelGroup_Found(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroupConfig{
			{Name: "g1"},
			{Name: "g2"},
		},
	}
	if g := FindModelGroup(cfg, "g1"); g == nil {
		t.Fatal("expected to find g1")
	}
	if g := FindModelGroup(cfg, "g2"); g == nil {
		t.Fatal("expected to find g2")
	}
}

func TestFindModelGroup_NotFound(t *testing.T) {
	cfg := &Config{}
	if g := FindModelGroup(cfg, "nonexistent"); g != nil {
		t.Fatal("expected nil for nonexistent group")
	}
}
