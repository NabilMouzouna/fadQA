package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var slugSanitizer = regexp.MustCompile(`[^a-zA-Z0-9.-]+`)

func fileName(store, appType string) string {
	s := slugSanitizer.ReplaceAllString(store, "_")
	a := slugSanitizer.ReplaceAllString(strings.ToLower(appType), "_")
	return fmt.Sprintf("%s__%s.json", s, a)
}

func filePath(dir, store, appType string) string {
	return filepath.Join(dir, fileName(store, appType))
}

// Load reads a store's cache file. ok=false means no usable cache exists
// (missing, corrupt, or schema mismatch) — callers should treat this as
// "run full". A corrupt file is preserved alongside as "<name>.bad" rather
// than silently discarded.
func Load(dir, store, appType string) (*StoreCache, bool) {
	p := filePath(dir, store, appType)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}

	var c StoreCache
	if jsonErr := json.Unmarshal(data, &c); jsonErr != nil {
		_ = os.WriteFile(p+".bad", data, 0o644)
		return nil, false
	}
	if c.SchemaVersion != SchemaVersion {
		return nil, false
	}
	if c.Products == nil {
		c.Products = map[string]ProductState{}
	}
	return &c, true
}

// Save atomically writes the cache to disk: write to a temp file, then
// rename over the real path (rename is atomic on the same filesystem),
// avoiding a corrupted cache if the process is killed mid-write.
func (c *StoreCache) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cache dir: %w", err)
	}
	p := filePath(dir, c.Store, c.AppType)
	tmp := p + ".tmp"

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write cache tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename cache: %w", err)
	}
	return nil
}

// FailingHandles returns handles whose last recorded verdict was a FAIL_*
// (and, if includeErrors, also ERROR) — the retest set for quick mode.
func (c *StoreCache) FailingHandles(includeErrors bool) []string {
	var out []string
	for h, st := range c.Products {
		if strings.HasPrefix(st.LastVerdict, "FAIL_") || (includeErrors && st.LastVerdict == "ERROR") {
			out = append(out, h)
		}
	}
	return out
}

// Upsert records a fresh test result for one product handle, preserving
// FirstSeen across runs and updating LastChanged only when the verdict
// actually flips.
func (c *StoreCache) Upsert(handle, title, url, verdictStr, reason string, now time.Time) {
	if c.Products == nil {
		c.Products = map[string]ProductState{}
	}
	existing, existed := c.Products[handle]
	st := ProductState{
		Handle:      handle,
		Title:       title,
		URL:         url,
		LastVerdict: verdictStr,
		LastReason:  reason,
		LastTested:  now,
	}
	switch {
	case existed && existing.LastVerdict == verdictStr:
		st.FirstSeen = existing.FirstSeen
		st.LastChanged = existing.LastChanged
	case existed:
		st.FirstSeen = existing.FirstSeen
		st.LastChanged = now
	default:
		st.FirstSeen = now
		st.LastChanged = now
	}
	c.Products[handle] = st
}

// DetectNew compares a freshly enumerated handle list against the cache,
// returning handles that are new (not yet cached) and handles that have
// disappeared (cached but absent from the current enumeration).
func (c *StoreCache) DetectNew(currentHandles []string) (newHandles, goneHandles []string) {
	current := make(map[string]bool, len(currentHandles))
	for _, h := range currentHandles {
		current[h] = true
		if _, ok := c.Products[h]; !ok {
			newHandles = append(newHandles, h)
		}
	}
	for h := range c.Products {
		if !current[h] {
			goneHandles = append(goneHandles, h)
		}
	}
	return newHandles, goneHandles
}

// KeywordsChanged reports whether freshly observed keyword lists differ
// from what's cached — a signal that quick-mode results may be stale
// because the store's config changed since the last full run.
func (c *StoreCache) KeywordsChanged(observedInclude, observedExclude []string) bool {
	return !stringSetEqual(c.RealiftKeywords, observedInclude) || !stringSetEqual(c.ExcludedKeywords, observedExclude)
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}
