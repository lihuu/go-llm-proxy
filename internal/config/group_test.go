package config

import (
	"testing"
	"time"
)

func TestGroupResolver_Resolve_Found(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
			{Name: "p2", Backend: "http://p2/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name:     "my-group",
				Strategy: "sequential",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
					{Provider: "p2", Model: "model-b"},
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	virtual, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected index 0, got: %d", idx)
	}
	if virtual.Name != "my-group" {
		t.Fatalf("expected virtual name my-group, got: %s", virtual.Name)
	}
	if virtual.Model != "model-a" {
		t.Fatalf("expected model model-a, got: %s", virtual.Model)
	}
	if virtual.Backend != "http://p1/v1" {
		t.Fatalf("expected backend http://p1/v1, got: %s", virtual.Backend)
	}
	if virtual.Type != "openai" {
		t.Fatalf("expected type openai, got: %s", virtual.Type)
	}
}

func TestGroupResolver_Resolve_NotFound(t *testing.T) {
	cfg := &Config{}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	_, _, err := resolver.Resolve("nonexistent", "")
	if err == nil {
		t.Fatal("expected error for nonexistent group")
	}
}

func TestGroupResolver_Resolve_SkipsProviderDown(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai", Status: "down"},
			{Name: "p2", Backend: "http://p2/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
					{Provider: "p2", Model: "model-b"},
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	virtual, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1 (skip p1), got: %d", idx)
	}
	if virtual.Model != "model-b" {
		t.Fatalf("expected model model-b, got: %s", virtual.Model)
	}
}

func TestGroupResolver_Resolve_AllProvidersDown(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai", Status: "down"},
			{Name: "p2", Backend: "http://p2/v1", Type: "openai", Status: "down"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
					{Provider: "p2", Model: "model-b"},
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	_, _, err := resolver.Resolve("my-group", "openai")
	if err == nil {
		t.Fatal("expected error when all providers are down")
	}
}

func TestGroupResolver_Resolve_TypeMismatch(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "anthropic"},
			{Name: "p2", Backend: "http://p2/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
					{Provider: "p2", Model: "model-b"},
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	// Requesting openai should skip p1 (anthropic) and use p2.
	virtual, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1 (skip anthropic), got: %d", idx)
	}
	if virtual.Model != "model-b" {
		t.Fatalf("expected model model-b, got: %s", virtual.Model)
	}
}

func TestGroupResolver_ResolveByIndex(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	virtual, idx, err := resolver.ResolveByIndex("my-group", 0, "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected index 0, got: %d", idx)
	}
	if virtual.Model != "model-a" {
		t.Fatalf("expected model model-a, got: %s", virtual.Model)
	}
}

func TestGroupResolver_ResolveByIndex_OutOfRange(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroupConfig{
			{Name: "my-group", Members: []ModelGroupMember{{Provider: "p1", Model: "m"}}},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	_, _, err := resolver.ResolveByIndex("my-group", 5, "")
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

func TestGroupResolver_CircuitBreaker_TripsAfterFailures(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
			{Name: "p2", Backend: "http://p2/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
					{Provider: "p2", Model: "model-b"},
				},
				CircuitBreaker: &CBSettings{
					FailureThreshold: 2,
					RecoverySeconds:  1, // short cooldown for testing
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	// First two failures on member 0 should trip the breaker.
	resolver.RecordFailure("my-group", 0, 500)
	resolver.RecordFailure("my-group", 0, 502)

	// Now Resolve should skip member 0 and use member 1.
	virtual, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1 (member 0 tripped), got: %d", idx)
	}
	if virtual.Model != "model-b" {
		t.Fatalf("expected model model-b, got: %s", virtual.Model)
	}
}

func TestGroupResolver_CircuitBreaker_RecoversAfterCooldown(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
				},
				CircuitBreaker: &CBSettings{
					FailureThreshold: 1,
					RecoverySeconds:  0, // immediate recovery
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	// Trip the breaker.
	resolver.RecordFailure("my-group", 0, 500)

	// Should still be down (cooldown is 0 but we need to wait).
	// With RecoverySeconds=0, the cooldown is already expired.
	// The breaker should transition to probing on the next Resolve.
	time.Sleep(10 * time.Millisecond)

	virtual, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error after cooldown, got: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected index 0 (recovered), got: %d", idx)
	}
	if virtual.Model != "model-a" {
		t.Fatalf("expected model model-a, got: %s", virtual.Model)
	}
}

func TestGroupResolver_RecordSuccess_ResetsBreaker(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
				},
				CircuitBreaker: &CBSettings{FailureThreshold: 2},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	// One failure (not enough to trip).
	resolver.RecordFailure("my-group", 0, 500)

	// Success should reset the counter.
	resolver.RecordSuccess("my-group", 0)

	// Another failure should not trip (counter was reset).
	resolver.RecordFailure("my-group", 0, 500)

	// Should still resolve to member 0.
	_, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected index 0 (not tripped), got: %d", idx)
	}
}

func TestGroupResolver_QuotaExhausted_LongerCooldown(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
			{Name: "p2", Backend: "http://p2/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
					{Provider: "p2", Model: "model-b"},
				},
				CircuitBreaker: &CBSettings{
					QuotaCooldownMinutes: 60, // long cooldown
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	// 429 should mark as quota exhausted.
	resolver.RecordFailure("my-group", 0, 429)

	// Resolve should skip member 0 and use member 1.
	virtual, idx, err := resolver.Resolve("my-group", "openai")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1 (member 0 quota exhausted), got: %d", idx)
	}
	if virtual.Model != "model-b" {
		t.Fatalf("expected model model-b, got: %s", virtual.Model)
	}
}

func TestGroupResolver_Resolve_NoAvailableMembers(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "p1", Backend: "http://p1/v1", Type: "openai"},
		},
		ModelGroups: []ModelGroupConfig{
			{
				Name: "my-group",
				Members: []ModelGroupMember{
					{Provider: "p1", Model: "model-a"},
				},
			},
		},
	}
	store := NewTestConfigStore(cfg)
	resolver := NewGroupResolver(store)

	// Trip the only member.
	resolver.RecordFailure("my-group", 0, 500)
	resolver.RecordFailure("my-group", 0, 500)
	resolver.RecordFailure("my-group", 0, 500)

	_, _, err := resolver.Resolve("my-group", "openai")
	if err == nil {
		t.Fatal("expected error when no members available")
	}
}
