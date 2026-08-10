package apps

import (
	"sync"
)

var (
	defaultSummaryOnce sync.Once
	defaultSummaries   []Summary
	defaultSummaryErr  error
)

// CachedSummaries returns catalog summaries for DefaultDirs, parsed once.
func CachedSummaries() ([]Summary, error) {
	defaultSummaryOnce.Do(func() {
		defaultSummaries, defaultSummaryErr = DefaultLoader().ListSummaries()
	})
	out := make([]Summary, len(defaultSummaries))
	copy(out, defaultSummaries)
	return out, defaultSummaryErr
}
