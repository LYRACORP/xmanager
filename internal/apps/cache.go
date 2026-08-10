package apps

import (
	"sync"
	"time"
)

var (
	summaryMu      sync.Mutex
	summaryCache   []Summary
	summaryErr     error
	summaryFetched time.Time
)

const summaryTTL = 30 * time.Minute

// CachedSummaries returns catalog summaries for DefaultLoader, refreshed periodically.
func CachedSummaries() ([]Summary, error) {
	summaryMu.Lock()
	defer summaryMu.Unlock()
	if summaryCache != nil && time.Since(summaryFetched) < summaryTTL {
		out := make([]Summary, len(summaryCache))
		copy(out, summaryCache)
		return out, summaryErr
	}
	sums, err := DefaultLoader().ListSummaries()
	summaryCache = sums
	summaryErr = err
	summaryFetched = time.Now()
	out := make([]Summary, len(sums))
	copy(out, sums)
	return out, err
}

// ResetSummaryCache clears the in-memory catalog cache (tests / forced refresh).
func ResetSummaryCache() {
	summaryMu.Lock()
	defer summaryMu.Unlock()
	summaryCache = nil
	summaryErr = nil
	summaryFetched = time.Time{}
}
