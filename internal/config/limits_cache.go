package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const modelLimitsCacheSchema = `
CREATE TABLE IF NOT EXISTS model_limits_cache (
	cache_key      TEXT PRIMARY KEY,
	model_name     TEXT NOT NULL,
	backend        TEXT NOT NULL,
	model_id       TEXT NOT NULL,
	backend_type   TEXT NOT NULL DEFAULT 'openai',
	context_window INTEGER NOT NULL DEFAULT 0,
	max_output     INTEGER NOT NULL DEFAULT 0,
	detected_at    TEXT NOT NULL
);
`

var initModelLimitsCacheOnce sync.Once

// modelLimitsCacheTTL is the maximum age of a cached entry before re-detection.
const modelLimitsCacheTTL = 24 * time.Hour

// initModelLimitsCache ensures the cache table exists. Idempotent — safe to
// call multiple times on the same or different databases.
func initModelLimitsCache(db *sql.DB) error {
	_, err := db.Exec(modelLimitsCacheSchema)
	return err
}

// modelLimitsCacheKey computes a deterministic key from the model's identity.
// Any change to name/backend/modelID/backendType produces a different key.
func modelLimitsCacheKey(name, backend, modelID, backendType string) string {
	h := sha256.Sum256([]byte(name + "\x00" + backend + "\x00" + modelID + "\x00" + backendType))
	return hex.EncodeToString(h[:])
}

// cachedModelLimits holds a previously detected result.
type cachedModelLimits struct {
	ContextWindow int
	MaxOutput     int
	DetectedAt    time.Time
}

// lookupModelLimitsCache returns cached limits if a fresh entry exists.
// Returns (nil, nil) on cache miss or expired entry.
func lookupModelLimitsCache(db *sql.DB, name, backend, modelID, backendType string) (*cachedModelLimits, error) {
	key := modelLimitsCacheKey(name, backend, modelID, backendType)
	var ctxWindow, maxOut int
	var detectedAtStr string
	err := db.QueryRow(`
		SELECT context_window, max_output, detected_at
		FROM model_limits_cache
		WHERE cache_key = ?
	`, key).Scan(&ctxWindow, &maxOut, &detectedAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	detectedAt, err := time.Parse(time.RFC3339, detectedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse detected_at: %w", err)
	}
	if time.Since(detectedAt) > modelLimitsCacheTTL {
		return nil, nil // expired, caller should re-detect
	}
	return &cachedModelLimits{
		ContextWindow: ctxWindow,
		MaxOutput:     maxOut,
		DetectedAt:    detectedAt,
	}, nil
}

// saveModelLimitsCache writes or updates a cache entry.
func saveModelLimitsCache(db *sql.DB, name, backend, modelID, backendType string, limits ModelLimits) {
	key := modelLimitsCacheKey(name, backend, modelID, backendType)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO model_limits_cache (cache_key, model_name, backend, model_id, backend_type, context_window, max_output, detected_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cache_key) DO UPDATE SET
			context_window = excluded.context_window,
			max_output = excluded.max_output,
			detected_at = excluded.detected_at
	`, key, name, backend, modelID, backendType, limits.ContextWindow, limits.MaxOutput, now)
	if err != nil {
		slog.Error("failed to save model limits cache", "model", name, "error", err)
	}
}
