package verdict

import "testing"

const fixturePass = `<!doctype html><html><head></head><body>
<div class="product-form">form stuff here</div>
<script type="application/json" id="realift-config">
{"account":"acc1","measurementId":"","app":"foot","sizeChart":"chart-1","style":"style-1"}
</script>
<script type="application/json" id="realift-debug-context">
{"product":{"id":123,"title":"Running Shoes","type":"Footwear"},"collections":[],"product_metafield_value":null,"product_metafield_present":false,"matched_keyword":"shoe","matched_field":"product_title","matched_value":"Running Shoes","resolution_source":"keyword_match","realift_keywords":{"shoe":"foot:chart-1:style-1"},"realift_keywords_present":true,"excluded_keywords":["sock"],"excluded_keywords_present":false,"is_excluded":false,"excluded_keyword":"","excluded_field":"","excluded_value":""}
</script>
<realift-button>
<style>@font-face{}</style>
<script type="application/realift">
{"id":123,"title":"Running Shoes","type":"Footwear"}
</script>
</realift-button>
</body></html>`

const fixtureNoButtonBlock = `<!doctype html><html><body>
<script type="application/json" id="realift-config">
{"account":"acc1","app":"foot","sizeChart":"chart-1","style":"style-1"}
</script>
<script type="application/json" id="realift-debug-context">
{"resolution_source":"keyword_match"}
</script>
</body></html>`

const fixtureSDKOff = `<!doctype html><html><body>
<div class="product">Just a normal Shopify page with no realift markers.</div>
<script>console.log("some other theme script");</script>
</body></html>`

const fixtureHidden = `<!doctype html><html><body>
<script type="application/json" id="realift-config">
{"account":"acc1","app":"foot","sizeChart":null,"style":null}
</script>
<script type="application/json" id="realift-debug-context">
{"resolution_source":"none","is_excluded":false}
</script>
<realift-button>
<script type="application/realift">{"id":1}</script>
</realift-button>
</body></html>`

// fixtureAttrOrderFlipped mirrors a theme that renders attributes in a
// different order and adds extras (e.g. a Shopify-injected data attribute)
// — extraction must be attribute-order independent.
const fixtureAttrOrderFlipped = `<!doctype html><html><body>
<script data-turbo-track="reload" id="realift-config" type="application/json">
{"account":"acc1","app":"hand","sizeChart":"c9","style":"s9"}
</script>
<realift-button data-foo="bar"></realift-button>
</body></html>`

func TestExtract_Pass(t *testing.T) {
	x := Extract([]byte(fixturePass))
	if !x.HasConfigTag || !x.HasDebugTag || !x.HasButton {
		t.Fatalf("expected all three signals present: %+v", x)
	}
	if x.Config == nil || IsEmptyJSON(x.Config.SizeChart) {
		t.Fatalf("expected non-empty sizeChart, got %+v", x.Config)
	}
	if x.Debug == nil || x.Debug.MatchedKeyword != "shoe" {
		t.Fatalf("expected matched_keyword=shoe, got %+v", x.Debug)
	}
}

func TestExtract_NoButtonBlock(t *testing.T) {
	x := Extract([]byte(fixtureNoButtonBlock))
	if !x.HasConfigTag {
		t.Fatalf("expected config tag present")
	}
	if x.HasButton {
		t.Fatalf("expected no button element")
	}
}

func TestExtract_SDKOff(t *testing.T) {
	x := Extract([]byte(fixtureSDKOff))
	if x.HasConfigTag {
		t.Fatalf("expected no config tag on a non-realift page")
	}
}

func TestExtract_Hidden(t *testing.T) {
	x := Extract([]byte(fixtureHidden))
	if !x.HasButton || x.Config == nil {
		t.Fatalf("expected button+config present: %+v", x)
	}
	if !IsEmptyJSON(x.Config.SizeChart) {
		t.Fatalf("expected empty sizeChart")
	}
	if x.Debug.ResolutionSource != "none" {
		t.Fatalf("expected resolution_source=none, got %q", x.Debug.ResolutionSource)
	}
}

func TestExtract_AttributeOrderIndependent(t *testing.T) {
	x := Extract([]byte(fixtureAttrOrderFlipped))
	if !x.HasConfigTag {
		t.Fatalf("expected config tag detected regardless of attribute order")
	}
	if x.Config == nil || x.Config.App != "hand" {
		t.Fatalf("expected app=hand, got %+v", x.Config)
	}
	if !x.HasButton {
		t.Fatalf("expected button element detected even with extra attributes")
	}
}

func TestLooksLikeSDKPresent(t *testing.T) {
	if LooksLikeSDKPresent([]byte(fixtureSDKOff)) {
		t.Fatalf("expected false for a page with no realift markers")
	}
	if !LooksLikeSDKPresent([]byte(fixturePass)) {
		t.Fatalf("expected true for a page with realift-config")
	}
}
