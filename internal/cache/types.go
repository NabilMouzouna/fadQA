package cache

import "time"

// SchemaVersion guards against loading a cache written by an incompatible
// version of the tool. On mismatch, Load discards the cache (forces a full
// run) rather than attempting a migration.
const SchemaVersion = 1

// StoreCache is the persisted, per-store-and-app-type QA context that
// enables "quick" reruns (retest only previously-failing products) without
// re-crawling the whole store.
type StoreCache struct {
	SchemaVersion    int                     `json:"schema_version"`
	Store            string                  `json:"store"`
	AppType          string                  `json:"app_type"`
	EnumMethod       string                  `json:"enum_method"`
	RealiftKeywords  []string                `json:"realift_keywords,omitempty"`
	ExcludedKeywords []string                `json:"excluded_keywords,omitempty"`
	LastFullRun      time.Time               `json:"last_full_run"`
	LastRun          time.Time               `json:"last_run"`
	Products         map[string]ProductState `json:"products"`
}

// ProductState is the last known test outcome for one product handle.
type ProductState struct {
	Handle      string    `json:"handle"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	LastVerdict string    `json:"last_verdict"`
	LastReason  string    `json:"last_reason"`
	FirstSeen   time.Time `json:"first_seen"`
	LastTested  time.Time `json:"last_tested"`
	LastChanged time.Time `json:"last_changed"`
}

// New creates an empty cache for a store/app-type pair.
func New(store, appType string) *StoreCache {
	return &StoreCache{
		SchemaVersion: SchemaVersion,
		Store:         store,
		AppType:       appType,
		Products:      map[string]ProductState{},
	}
}
