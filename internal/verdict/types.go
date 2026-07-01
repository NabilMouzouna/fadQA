package verdict

import (
	"encoding/json"
	"sort"
)

// Config mirrors the JSON rendered by Liquid into
// <script type="application/json" id="realift-config"> on every product page.
type Config struct {
	Account       string          `json:"account"`
	MeasurementID string          `json:"measurementId"`
	App           string          `json:"app"`
	SizeChart     json.RawMessage `json:"sizeChart"`
	Style         json.RawMessage `json:"style"`
}

// DebugCollection is one entry of the "collections" array in the debug context.
type DebugCollection struct {
	ID               any    `json:"id"`
	Title            string `json:"title"`
	MetafieldValue   any    `json:"metafield_value"`
	MetafieldPresent bool   `json:"metafield_present"`
}

// DebugProduct is the "product" object embedded in the debug context.
type DebugProduct struct {
	ID    any    `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// DebugContext mirrors <script type="application/json" id="realift-debug-context">.
type DebugContext struct {
	Product                  DebugProduct      `json:"product"`
	Collections              []DebugCollection `json:"collections"`
	ProductMetafieldValue    any               `json:"product_metafield_value"`
	ProductMetafieldPresent  bool              `json:"product_metafield_present"`
	MatchedKeyword           string            `json:"matched_keyword"`
	MatchedField             string            `json:"matched_field"`
	MatchedValue             string            `json:"matched_value"`
	ResolutionSource         string            `json:"resolution_source"`
	ResolutionCollectionID   any               `json:"resolution_collection_id"`
	ResolutionCollectionName string            `json:"resolution_collection_title"`
	RealiftKeywords          any               `json:"realift_keywords"`
	RealiftKeywordsPresent   bool              `json:"realift_keywords_present"`
	ExcludedKeywords         []string          `json:"excluded_keywords"`
	ExcludedKeywordsPresent  bool              `json:"excluded_keywords_present"`
	IsExcluded               bool              `json:"is_excluded"`
	ExcludedKeyword          string            `json:"excluded_keyword"`
	ExcludedField            string            `json:"excluded_field"`
	ExcludedValue            string            `json:"excluded_value"`
}

// IncludeKeywordList extracts the configured include-keyword strings from
// the raw realift_keywords value. Liquid serializes app.metafields.
// sizechart.keywords.value as a JSON object mapping keyword ->
// "app:sizeChart:style", so a plain []string cast won't work — this decodes
// the map shape (and tolerates an array shape, in case that ever changes).
func (d *DebugContext) IncludeKeywordList() []string {
	if d == nil || d.RealiftKeywords == nil {
		return nil
	}
	switch v := d.RealiftKeywords.(type) {
	case map[string]any:
		out := make([]string, 0, len(v))
		for k := range v {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// Extracted holds everything pulled out of one product page's raw HTML.
type Extracted struct {
	HasConfigTag bool
	HasDebugTag  bool
	HasButton    bool
	Config       *Config
	Debug        *DebugContext
	// Truncated is set when a script tag was found but its JSON body looked
	// incomplete (likely cut off by the body size cap).
	Truncated bool
}

// Verdict is the final classification for one product page.
type Verdict string

const (
	PASS               Verdict = "PASS"
	WarnUnexpectedShow Verdict = "WARN_UNEXPECTED_VISIBLE"
	FailSDKOff         Verdict = "FAIL_SDK_OFF"
	FailNoButtonBlock  Verdict = "FAIL_NO_BUTTON_BLOCK"
	FailNotIncluded    Verdict = "FAIL_NOT_INCLUDED"
	FailExcluded       Verdict = "FAIL_EXCLUDED"
	SkipNotRelevant    Verdict = "SKIP_NOT_RELEVANT"
	Gone               Verdict = "GONE"
	Errored            Verdict = "ERROR"
)

// IsFail reports whether a verdict counts toward the "failing products" bucket.
func (v Verdict) IsFail() bool {
	switch v {
	case FailSDKOff, FailNoButtonBlock, FailNotIncluded, FailExcluded:
		return true
	default:
		return false
	}
}

// ProductResult is the outcome of testing a single product page.
type ProductResult struct {
	Handle       string
	Title        string
	URL          string
	ProductType  string
	Verdict      Verdict
	Reason       string
	SuggestedFix string
	HTTPStatus   int
	AppType      string

	MatchedKeyword  string
	ExcludedKeyword string

	// Advisory holds a secondary, non-authoritative flag (e.g.
	// WarnUnexpectedShow) that never changes Verdict/IsFail() but is
	// surfaced in the report as something worth a human glance.
	Advisory Verdict

	// Debug carries the full parsed debug context when available, so
	// callers (report/cache) can do store-level aggregation such as
	// "no include keywords configured at all".
	Debug *DebugContext
}
