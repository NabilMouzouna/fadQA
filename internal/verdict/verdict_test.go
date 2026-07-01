package verdict

import (
	"encoding/json"
	"strings"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestIsEmptyJSON(t *testing.T) {
	cases := map[string]bool{
		"":         true,
		"null":     true,
		`""`:       true,
		"{}":       true,
		"[]":       true,
		"   ":      true,
		`"shoe"`:   false,
		"123":      false,
		`{"a":1}`:  false,
		`["shoe"]`: false,
	}
	for input, want := range cases {
		if got := IsEmptyJSON(json.RawMessage(input)); got != want {
			t.Errorf("IsEmptyJSON(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestClassify_SDKOff(t *testing.T) {
	res := Classify(Input{Handle: "h1", Title: "Running Shoes"}, Extracted{HasConfigTag: false}, "realfoot")
	if res.Verdict != FailSDKOff {
		t.Fatalf("want FailSDKOff, got %s (%s)", res.Verdict, res.Reason)
	}
}

func TestClassify_SDKOff_TakesPrecedenceOverStrayButton(t *testing.T) {
	// A <realift-button> present without a config tag must never PASS —
	// the client JS can't populate it without #realift-config.
	x := Extracted{HasConfigTag: false, HasButton: true}
	res := Classify(Input{Handle: "h1"}, x, "realfoot")
	if res.Verdict != FailSDKOff {
		t.Fatalf("want FailSDKOff even with stray button, got %s", res.Verdict)
	}
}

func TestClassify_NoButtonBlock(t *testing.T) {
	x := Extracted{HasConfigTag: true, HasButton: false, Config: &Config{}}
	res := Classify(Input{Handle: "h1"}, x, "realfoot")
	if res.Verdict != FailNoButtonBlock {
		t.Fatalf("want FailNoButtonBlock, got %s", res.Verdict)
	}
}

func TestClassify_ConfigParseFailed(t *testing.T) {
	x := Extracted{HasConfigTag: true, HasButton: true, Config: nil}
	res := Classify(Input{Handle: "h1"}, x, "realfoot")
	if res.Verdict != Errored {
		t.Fatalf("want Errored, got %s", res.Verdict)
	}
}

func TestClassify_Pass(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`"chart-1"`)},
		Debug:  &DebugContext{MatchedKeyword: "shoe"},
	}
	res := Classify(Input{Handle: "h1", Title: "Running Shoes"}, x, "realfoot")
	if res.Verdict != PASS {
		t.Fatalf("want PASS, got %s", res.Verdict)
	}
	if res.Advisory != "" {
		t.Fatalf("expected no advisory, got %s", res.Advisory)
	}
}

func TestClassify_PassButUnexpectedForAppType(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`"chart-1"`)},
		Debug:  &DebugContext{},
	}
	res := Classify(Input{Handle: "h1", Title: "Wool Socks"}, x, "realfoot")
	if res.Verdict != PASS {
		t.Fatalf("want PASS, got %s", res.Verdict)
	}
	if res.Advisory != WarnUnexpectedShow {
		t.Fatalf("want advisory WarnUnexpectedShow, got %q", res.Advisory)
	}
}

func TestClassify_HiddenNoDebugContext(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
	}
	res := Classify(Input{Handle: "h1"}, x, "realfoot")
	if res.Verdict != FailNotIncluded {
		t.Fatalf("want FailNotIncluded, got %s", res.Verdict)
	}
}

func TestClassify_UnmatchedRelevant(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug:  &DebugContext{ResolutionSource: "none"},
	}
	res := Classify(Input{Handle: "h1", Title: "Running Shoes"}, x, "realfoot")
	if res.Verdict != FailNotIncluded {
		t.Fatalf("want FailNotIncluded, got %s", res.Verdict)
	}
	if !strings.Contains(res.SuggestedFix, "shoe") {
		t.Fatalf("expected suggested fix to mention the shoe hint, got %q", res.SuggestedFix)
	}
}

func TestClassify_UnmatchedIrrelevant(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug:  &DebugContext{ResolutionSource: "none"},
	}
	res := Classify(Input{Handle: "h1", Title: "Wool Socks"}, x, "realfoot")
	if res.Verdict != SkipNotRelevant {
		t.Fatalf("want SkipNotRelevant, got %s", res.Verdict)
	}
}

func TestClassify_UnmatchedUnknownRelevance(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug:  &DebugContext{ResolutionSource: "none"},
	}
	// Nothing in the title hints either way — default to FAIL (assume
	// relevant), per the "never manufacture a fail, but never manufacture a
	// skip either" rule: an unmatched product defaults to actionable.
	res := Classify(Input{Handle: "h1", Title: "Item 42B"}, x, "realfoot")
	if res.Verdict != FailNotIncluded {
		t.Fatalf("want FailNotIncluded for unknown relevance, got %s", res.Verdict)
	}
}

func TestClassify_ExcludedHardcodedFallback_FootApp(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug: &DebugContext{
			ResolutionSource:        "excluded",
			IsExcluded:              true,
			ExcludedKeyword:         "sock",
			ExcludedKeywordsPresent: false, // store hasn't customized -> hardcoded fallback in use
		},
	}
	res := Classify(Input{Handle: "h1", Title: "Wool Socks"}, x, "realfoot")
	if res.Verdict != SkipNotRelevant {
		t.Fatalf("want SkipNotRelevant (correct hide for realfoot), got %s: %s", res.Verdict, res.Reason)
	}
}

func TestClassify_ExcludedHardcodedFallback_Foot3D(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug: &DebugContext{
			ResolutionSource:        "excluded",
			IsExcluded:              true,
			ExcludedKeyword:         "insole",
			ExcludedKeywordsPresent: false,
		},
	}
	res := Classify(Input{Handle: "h1", Title: "Comfort Insole"}, x, "foot3d")
	if res.Verdict != SkipNotRelevant {
		t.Fatalf("want SkipNotRelevant for foot3d, got %s", res.Verdict)
	}
}

func TestClassify_ExcludedHardcodedFallback_NonFootApp(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug: &DebugContext{
			ResolutionSource:        "excluded",
			IsExcluded:              true,
			ExcludedKeyword:         "sock",
			ExcludedKeywordsPresent: false,
		},
	}
	res := Classify(Input{Handle: "h1", Title: "Ankle Socks"}, x, "realbody")
	if res.Verdict != FailExcluded {
		t.Fatalf("want FailExcluded (hardcoded fallback may be wrong for realbody), got %s", res.Verdict)
	}
}

func TestClassify_ExcludedByStoreKeyword(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug: &DebugContext{
			ResolutionSource:        "excluded",
			IsExcluded:              true,
			ExcludedKeyword:         "limited-edition",
			ExcludedKeywordsPresent: true,
			ExcludedValue:           "Limited Edition Runner",
		},
	}
	res := Classify(Input{Handle: "h1", Title: "Limited Edition Runner"}, x, "realfoot")
	if res.Verdict != FailExcluded {
		t.Fatalf("want FailExcluded, got %s", res.Verdict)
	}
	if !strings.Contains(res.SuggestedFix, "limited-edition") {
		t.Fatalf("expected suggested fix to name the keyword, got %q", res.SuggestedFix)
	}
}

func TestClassify_ResolutionSourceInconsistency(t *testing.T) {
	x := Extracted{
		HasConfigTag: true, HasButton: true,
		Config: &Config{SizeChart: raw(`null`)},
		Debug:  &DebugContext{ResolutionSource: "product_metafield"},
	}
	res := Classify(Input{Handle: "h1"}, x, "realfoot")
	if res.Verdict != FailNotIncluded {
		t.Fatalf("want FailNotIncluded (anomaly case), got %s", res.Verdict)
	}
}

func TestIncludeKeywordList(t *testing.T) {
	d := &DebugContext{RealiftKeywords: map[string]any{"shoe": "foot:c1:s1", "boot": "foot:c1:s1"}}
	got := d.IncludeKeywordList()
	if len(got) != 2 || got[0] != "boot" || got[1] != "shoe" {
		t.Fatalf("unexpected keyword list: %v", got)
	}
}

func TestIncludeKeywordList_Nil(t *testing.T) {
	var d *DebugContext
	if got := d.IncludeKeywordList(); got != nil {
		t.Fatalf("want nil for nil receiver, got %v", got)
	}
}
