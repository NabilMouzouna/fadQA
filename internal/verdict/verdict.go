package verdict

import "fmt"

// Input is the minimal per-product context the classifier needs, decoupled
// from the enumerate package so verdict has no import dependency on it.
type Input struct {
	Handle      string
	Title       string
	URL         string
	ProductType string
}

// Classify turns one product page's extracted HTML signals into a
// ProductResult. It never re-derives the ground-truth show/hide decision —
// that is fully determined by Config.SizeChart — and only uses the
// app-type relevance dictionary to annotate or soften, never to override it.
func Classify(in Input, x Extracted, appType string) ProductResult {
	res := ProductResult{
		Handle:      in.Handle,
		Title:       in.Title,
		URL:         in.URL,
		ProductType: in.ProductType,
		AppType:     appType,
		Debug:       x.Debug,
	}
	if x.Debug != nil && res.Title == "" {
		res.Title = x.Debug.Product.Title
	}
	if x.Debug != nil && res.ProductType == "" {
		res.ProductType = x.Debug.Product.Type
	}

	if !x.HasConfigTag {
		res.Verdict = FailSDKOff
		res.Reason = "No realift-config found on this page — the Realift SDK app embed is disabled or not installed for this theme."
		res.SuggestedFix = "Enable the Realift app embed in the theme editor (Online Store > Themes > Customize > App embeds)."
		return res
	}

	if !x.HasButton {
		res.Verdict = FailNoButtonBlock
		res.Reason = "Realift SDK is enabled but no <realift-button> element was found on this product's page — the button block isn't added to this product's template."
		res.SuggestedFix = "Add the Realift Button block to the product template this page uses (check for alternate product templates)."
		return res
	}

	if x.Config == nil {
		res.Verdict = Errored
		res.Reason = "realift-config tag was present but its JSON body could not be parsed."
		return res
	}

	res.HTTPStatus = 200

	if !IsEmptyJSON(x.Config.SizeChart) {
		res.Verdict = PASS
		res.Reason = "Button resolves a size chart and is visible."
		if x.Debug != nil {
			res.MatchedKeyword = x.Debug.MatchedKeyword
			guess, hint := guessAppRelevance(appType, res.Title, res.ProductType)
			if guess == guessIrrelevant {
				res.Advisory = WarnUnexpectedShow
				res.SuggestedFix = fmt.Sprintf("Button is visible but the product looks out-of-scope for %s (matched hint %q). Consider adding an exclude keyword.", appType, hint)
			}
		}
		return res
	}

	// Hidden: sizeChart is empty. Determine why from the debug context.
	if x.Debug == nil {
		res.Verdict = FailNotIncluded
		res.Reason = "Button is hidden (no size chart resolved) and no debug context was available to determine why."
		return res
	}

	res.ExcludedKeyword = x.Debug.ExcludedKeyword

	switch x.Debug.ResolutionSource {
	case "excluded":
		classifyExcluded(&res, x, appType)
	case "none":
		classifyUnmatched(&res, x, appType)
	default:
		// e.g. "product_metafield"/"collection_metafield" claimed a
		// resolution yet sizeChart came back empty — an inconsistency,
		// most likely a metafield pointing at a deleted/renamed size chart.
		res.Verdict = FailNotIncluded
		res.Reason = fmt.Sprintf("Button is hidden, but resolution_source=%q suggested a match — likely a size chart metafield pointing at a missing/renamed size chart.", x.Debug.ResolutionSource)
		res.SuggestedFix = "Check the product/collection metafield realift.sizeChart still points at a valid size chart."
	}

	return res
}

func classifyExcluded(res *ProductResult, x Extracted, appType string) {
	kw := x.Debug.ExcludedKeyword
	usingHardcodedFallback := !x.Debug.ExcludedKeywordsPresent || hardcodedFallbackExclusions[kw]

	if usingHardcodedFallback && isFootApp(appType) {
		res.Verdict = SkipNotRelevant
		res.Reason = fmt.Sprintf("Correctly hidden — %q is out of scope for %s sizing (default exclusion list).", kw, appType)
		return
	}

	if usingHardcodedFallback {
		res.Verdict = FailExcluded
		res.Reason = fmt.Sprintf("Hidden by the default fallback exclusion keyword %q, which may be inappropriate for %s.", kw, appType)
		res.SuggestedFix = fmt.Sprintf("Configure an explicit excluded-keywords list for this store so %q doesn't apply to %s products, or rename the product to avoid the term.", kw, appType)
		return
	}

	res.Verdict = FailExcluded
	res.Reason = fmt.Sprintf("Excluded by store exclude-keyword %q (matched product title %q).", kw, x.Debug.ExcludedValue)
	res.SuggestedFix = fmt.Sprintf("Remove %q from the store's excluded keywords if this product should show the button.", kw)
}

func classifyUnmatched(res *ProductResult, x Extracted, appType string) {
	guess, hint := guessAppRelevance(appType, res.Title, res.ProductType)

	if guess == guessIrrelevant {
		res.Verdict = SkipNotRelevant
		res.Reason = fmt.Sprintf("Hidden, and appears out of scope for %s (matched hint %q). No keyword configured either way — verify.", appType, hint)
		return
	}

	res.Verdict = FailNotIncluded
	if guess == guessRelevant {
		res.Reason = fmt.Sprintf("Hidden — no include keyword matched, but the product looks relevant to %s (matched hint %q).", appType, hint)
		res.SuggestedFix = fmt.Sprintf("Add %q (or the product's title/type) as an include keyword for %s.", hint, appType)
		return
	}

	res.Reason = "Hidden — no include keyword, product metafield, or collection metafield resolved a size chart for this product."
	res.SuggestedFix = "Add an include keyword matching this product's title/type/collection, or set the realift.sizeChart metafield directly."
}
