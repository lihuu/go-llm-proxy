package handler

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"go-llm-proxy/internal/api"
	"go-llm-proxy/internal/usage"
)

// ttfbReader wraps an io.ReadCloser and records the wall-clock time of the
// first successful Read call. Safe for concurrent use; the TTFB is captured
// atomically on the first read and never overwritten.
type ttfbReader struct {
	rc        io.ReadCloser
	startTime time.Time
	ttfbMs    atomic.Int64
}

func newTTFBReader(rc io.ReadCloser, startTime time.Time) *ttfbReader {
	return &ttfbReader{rc: rc, startTime: startTime}
}

func (r *ttfbReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 && r.ttfbMs.Load() == 0 {
		r.ttfbMs.Store(time.Since(r.startTime).Milliseconds())
	}
	return n, err
}

func (r *ttfbReader) Close() error {
	return r.rc.Close()
}

func (r *ttfbReader) TTFBMs() int64 {
	return r.ttfbMs.Load()
}

// extractTTFB extracts the time-to-first-byte from a response body that
// has been wrapped with newTTFBReader. Returns 0 if the body was not wrapped.
func extractTTFB(resp *http.Response) int64 {
	if tr, ok := resp.Body.(*ttfbReader); ok {
		return tr.TTFBMs()
	}
	return 0
}

// Unified usage-logging helpers.
//
// Every handler needs to emit a usage.UsageRecord after a request completes.
// The record's fields are nearly identical regardless of path: a few
// per-request values (timestamp, key, model, endpoint, status, byte counts)
// plus token counts that come from one of two sources — `*api.ChunkUsage`
// for Chat-Completions-shaped backends or `*converseUsage` for Bedrock.
//
// Before this helper there were 10+ hand-rolled record constructions across
// the handlers, which is exactly how fields drift out of sync. Route
// everything through `logUsage` and drift becomes a compile error.

type usageLogInput struct {
	startTime       time.Time
	statusCode      int
	keyName         string
	keyHash         string
	model           string
	endpoint        string
	requestBytes    int64
	responseBytes   int64
	inputTokens     int
	outputTokens    int
	totalTokens     int
	cacheReadTokens  int
	cacheWriteTokens int
	ttfbMs          int64 // time-to-first-byte in ms; 0 = not captured
}

// logUsage writes a single usage record. Safe to call with ul==nil.
// Emission is deferred to a goroutine so the caller's hot path is not
// blocked by SQLite contention.
func logUsage(ul *usage.UsageLogger, in usageLogInput) {
	if ul == nil {
		return
	}
	rec := usage.UsageRecord{
		Timestamp:       in.startTime,
		KeyHash:         in.keyHash,
		KeyName:         in.keyName,
		Model:           in.model,
		Endpoint:        in.endpoint,
		StatusCode:      in.statusCode,
		RequestBytes:    in.requestBytes,
		ResponseBytes:   in.responseBytes,
		InputTokens:     in.inputTokens,
		OutputTokens:    in.outputTokens,
		TotalTokens:     in.totalTokens,
		CacheReadTokens:  in.cacheReadTokens,
		CacheWriteTokens: in.cacheWriteTokens,
		DurationMS:      time.Since(in.startTime).Milliseconds(),
		TTFBMs:          in.ttfbMs,
	}
	go ul.Log(rec)
}

// logUsageChat is the Chat-Completions adapter: extract tokens from
// *api.ChunkUsage (if non-nil) and emit.
func logUsageChat(ul *usage.UsageLogger, in usageLogInput, u *api.ChunkUsage) {
	if u != nil {
		in.inputTokens = u.PromptTokens
		in.outputTokens = u.CompletionTokens
		in.totalTokens = u.TotalTokens
		if u.PromptTokensDetails != nil {
			in.cacheReadTokens = u.PromptTokensDetails.CachedTokens
		}
	}
	logUsage(ul, in)
}

// logUsageConverse is the Bedrock Converse adapter: extract tokens from
// *converseUsage (if non-nil) and emit.
func logUsageConverse(ul *usage.UsageLogger, in usageLogInput, u *converseUsage) {
	if u != nil {
		in.inputTokens = u.Input
		in.outputTokens = u.Output
		in.totalTokens = u.Input + u.Output + u.CacheReadInput + u.CacheWriteInput
		in.cacheReadTokens = u.CacheReadInput
		in.cacheWriteTokens = u.CacheWriteInput
	}
	logUsage(ul, in)
}
