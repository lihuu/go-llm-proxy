package config

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Default circuit-breaker settings.
const (
	defaultFailureThreshold     = 3
	defaultRecoverySeconds      = 30
	defaultQuotaCooldownMinutes = 60
)

// memberState represents the circuit-breaker state for a single group member.
type memberState int

const (
	stateUp            memberState = iota // normal operation
	stateDown                             // consecutive failures tripped the breaker
	stateQuotaExhausted                   // HTTP 429 — quota exhausted
	stateProbing                          // cooldown expired, next request is a probe
)

// memberBreaker tracks the circuit-breaker state for one group member.
type memberBreaker struct {
	state        memberState
	failCount    int
	lastFailTime time.Time
	mu           sync.Mutex
}

// GroupResolver resolves model group names to virtual ModelConfigs and
// tracks per-member circuit-breaker state for failover.
type GroupResolver struct {
	store  *ConfigStore
	states map[string]map[int]*memberBreaker // groupName → memberIndex → breaker
	mu     sync.RWMutex
}

// NewGroupResolver creates a GroupResolver backed by the given ConfigStore.
func NewGroupResolver(store *ConfigStore) *GroupResolver {
	return &GroupResolver{
		store:  store,
		states: make(map[string]map[int]*memberBreaker),
	}
}

// getBreaker returns (or creates) the breaker for the given group member.
func (r *GroupResolver) getBreaker(groupName string, memberIndex int) *memberBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	groupMap, ok := r.states[groupName]
	if !ok {
		groupMap = make(map[int]*memberBreaker)
		r.states[groupName] = groupMap
	}
	b, ok := groupMap[memberIndex]
	if !ok {
		b = &memberBreaker{state: stateUp}
		groupMap[memberIndex] = b
	}
	return b
}

// Resolve selects the first available member of a model group and returns a
// virtual ModelConfig for it. Members are skipped when:
//   - the referenced provider is manually marked "down"
//   - the circuit breaker is tripped (down or quota_exhausted) and the
//     cooldown has not yet expired
//
// The returned ModelConfig is a shallow copy suitable for a single request;
// callers must not store it across requests.
func (r *GroupResolver) Resolve(groupName string, typeHint string) (*ModelConfig, int, error) {
	cfg := r.store.Get()

	group := FindModelGroup(cfg, groupName)
	if group == nil {
		return nil, -1, fmt.Errorf("model group %q not found", groupName)
	}

	for i := range group.Members {
		virtual, err := r.resolveMember(group, i, typeHint)
		if err != nil {
			continue
		}
		if virtual != nil {
			return virtual, i, nil
		}
	}

	return nil, -1, fmt.Errorf("no available members in model group %q", groupName)
}

// ResolveByIndex resolves a specific member of a model group by index,
// returning a virtual ModelConfig. Returns an error if the member is
// unavailable (provider down, breaker tripped, type mismatch).
func (r *GroupResolver) ResolveByIndex(groupName string, memberIndex int, typeHint string) (*ModelConfig, int, error) {
	cfg := r.store.Get()
	group := FindModelGroup(cfg, groupName)
	if group == nil {
		return nil, -1, fmt.Errorf("model group %q not found", groupName)
	}
	if memberIndex < 0 || memberIndex >= len(group.Members) {
		return nil, -1, fmt.Errorf("member index %d out of range for group %q", memberIndex, groupName)
	}

	virtual, err := r.resolveMember(group, memberIndex, typeHint)
	if err != nil {
		return nil, -1, err
	}
	return virtual, memberIndex, nil
}

// resolveMember checks availability of a single group member and returns a
// virtual ModelConfig if the member is usable. Returns nil without error if
// the member is skipped (provider down, breaker tripped, type mismatch).
func (r *GroupResolver) resolveMember(group *ModelGroupConfig, memberIndex int, typeHint string) (*ModelConfig, error) {
	cfg := r.store.Get()
	member := group.Members[memberIndex]

	// Skip members whose provider is manually marked "down".
	provider := findProvider(cfg, member.Provider)
	if provider == nil {
		return nil, fmt.Errorf("provider %q not found", member.Provider)
	}
	if provider.Status == "down" {
		slog.Debug("group member provider is manually down, skipping",
			"group", group.Name, "member_index", memberIndex, "provider", member.Provider)
		return nil, nil
	}

	// Check circuit breaker state.
	b := r.getBreaker(group.Name, memberIndex)
	b.mu.Lock()
	state := b.state
	lastFail := b.lastFailTime
	b.mu.Unlock()

	cb := group.CircuitBreaker
	recoveryDur := time.Duration(defaultRecoverySeconds) * time.Second
	quotaDur := time.Duration(defaultQuotaCooldownMinutes) * time.Minute
	if cb != nil {
		recoveryDur = time.Duration(cb.RecoverySeconds) * time.Second
		quotaDur = time.Duration(cb.QuotaCooldownMinutes) * time.Minute
	}

	switch state {
	case stateDown:
		if time.Since(lastFail) < recoveryDur {
			slog.Debug("group member breaker is down, skipping",
				"group", group.Name, "member_index", memberIndex, "provider", member.Provider)
			return nil, nil
		}
		// Cooldown expired — transition to probing.
		b.mu.Lock()
		if b.state == stateDown {
			b.state = stateProbing
			slog.Info("group member breaker recovering, entering probing",
				"group", group.Name, "member_index", memberIndex, "provider", member.Provider)
		}
		b.mu.Unlock()

	case stateQuotaExhausted:
		if time.Since(lastFail) < quotaDur {
			slog.Debug("group member quota exhausted, skipping",
				"group", group.Name, "member_index", memberIndex, "provider", member.Provider)
			return nil, nil
		}
		// Cooldown expired — transition to probing.
		b.mu.Lock()
		if b.state == stateQuotaExhausted {
			b.state = stateProbing
			slog.Info("group member quota cooldown expired, entering probing",
				"group", group.Name, "member_index", memberIndex, "provider", member.Provider)
		}
		b.mu.Unlock()

	case stateProbing, stateUp:
		// Usable.
	}

	// Build a virtual ModelConfig from the provider config.
	virtual := &ModelConfig{
		Name:     group.Name,
		Model:    member.Model,
		Provider: member.Provider,
		Backend:  provider.Backend,
		Type:     provider.Type,
		APIKey:   provider.APIKey,
		AuthType: provider.AuthType,
		Timeout:  300,
		// Bedrock fields inherited from provider.
		Region:          provider.Region,
		AWSAccessKey:    provider.AWSAccessKey,
		AWSSecretKey:    provider.AWSSecretKey,
		AWSSessionToken: provider.AWSSessionToken,
		GuardrailID:      provider.GuardrailID,
		GuardrailVersion: provider.GuardrailVersion,
		GuardrailTrace:   provider.GuardrailTrace,
	}

	// Apply Bedrock defaults if applicable.
	if virtual.Type == BackendBedrock {
		applyBedrockDefaults(virtual)
	}

	// Validate type hint compatibility.
	if typeHint != "" && virtual.Type != "" && virtual.Type != typeHint {
		slog.Debug("group member type mismatch with request, skipping",
			"group", group.Name, "member_index", memberIndex,
			"provider", member.Provider,
			"member_type", virtual.Type,
			"request_type", typeHint)
		return nil, nil
	}

	slog.Debug("resolved group member",
		"group", group.Name, "member_index", memberIndex,
		"provider", member.Provider, "model", member.Model)
	return virtual, nil
}

// RecordFailure records a failure for the given group member. HTTP 429 is
// treated as quota exhaustion (longer cooldown); all other failures
// increment the consecutive-failure counter.
func (r *GroupResolver) RecordFailure(groupName string, memberIndex int, statusCode int) {
	b := r.getBreaker(groupName, memberIndex)
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastFailTime = time.Now()

	if statusCode == 429 {
		b.state = stateQuotaExhausted
		b.failCount = 0 // reset counter; 429 is not a "failure" in the same sense
		slog.Warn("group member quota exhausted",
			"group", groupName, "member_index", memberIndex)
		return
	}

	b.failCount++
	cfg := r.store.Get()
	group := FindModelGroup(cfg, groupName)
	threshold := defaultFailureThreshold
	if group != nil && group.CircuitBreaker != nil && group.CircuitBreaker.FailureThreshold > 0 {
		threshold = group.CircuitBreaker.FailureThreshold
	}

	if b.failCount >= threshold {
		b.state = stateDown
		slog.Warn("group member breaker tripped",
			"group", groupName, "member_index", memberIndex,
			"fail_count", b.failCount, "threshold", threshold)
	} else {
		slog.Debug("group member failure recorded",
			"group", groupName, "member_index", memberIndex,
			"fail_count", b.failCount, "threshold", threshold)
	}
}

// RecordSuccess records a success for the given group member, resetting the
// circuit breaker to the up state.
func (r *GroupResolver) RecordSuccess(groupName string, memberIndex int) {
	b := r.getBreaker(groupName, memberIndex)
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state != stateUp {
		slog.Info("group member recovered",
			"group", groupName, "member_index", memberIndex,
			"previous_state", b.state)
	}
	b.state = stateUp
	b.failCount = 0
}

// findProvider looks up a ProviderConfig by name.
func findProvider(cfg *Config, name string) *ProviderConfig {
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == name {
			return &cfg.Providers[i]
		}
	}
	return nil
}
