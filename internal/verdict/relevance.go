package verdict

import "strings"

// appHints are tunable, advisory-only keyword lists used to guess whether a
// product is in-scope for a given realift app type. They never override a
// ground-truth signal from the page itself (a resolved sizeChart is always a
// PASS; these hints can only soften an unmatched/excluded hide into
// SKIP_NOT_RELEVANT, or flag an unexpectedly-visible PASS for review).
type appHints struct {
	relevant   []string
	irrelevant []string
}

var appHintTable = map[string]appHints{
	"realfoot": {
		relevant: []string{
			"shoe", "shoes", "sneaker", "sneakers", "boot", "boots", "sandal", "sandals",
			"footwear", "loafer", "loafers", "heel", "heels", "cleat", "cleats",
			"trainer", "trainers", "flip flop", "flip-flop", "slipper", "slippers", "moccasin",
		},
		irrelevant: []string{
			"sock", "socks", "insole", "outsole", "shirt", "t-shirt", "tshirt", "hat", "cap",
			"glove", "gloves", "belt", "wallet", "bag", "backpack", "watch", "ring",
			"necklace", "sunglasses", "scarf", "purse",
		},
	},
	"foot3d": {
		relevant: []string{
			"shoe", "shoes", "sneaker", "sneakers", "boot", "boots", "sandal", "sandals",
			"footwear", "loafer", "loafers", "heel", "heels", "cleat", "cleats", "trainer", "trainers",
		},
		irrelevant: []string{
			"sock", "socks", "insole", "outsole", "shirt", "t-shirt", "tshirt", "hat", "cap",
			"glove", "gloves", "belt", "wallet", "bag", "backpack", "watch", "ring",
		},
	},
	"realhand": {
		relevant: []string{
			"glove", "gloves", "mitten", "mittens", "watch", "bracelet", "ring",
		},
		irrelevant: []string{
			"shoe", "shoes", "sneaker", "sneakers", "boot", "boots", "sock", "socks",
			"shirt", "t-shirt", "tshirt", "hat", "cap", "belt",
		},
	},
	"realbody": {
		relevant: []string{
			"shirt", "t-shirt", "tshirt", "dress", "pant", "pants", "jean", "jeans",
			"jacket", "coat", "suit", "sweater", "hoodie", "skirt", "legging", "leggings",
			"bra", "underwear", "swimsuit", "romper", "jumpsuit",
		},
		irrelevant: []string{
			"shoe", "shoes", "sneaker", "sneakers", "boot", "boots", "sock", "socks",
			"glove", "gloves", "wallet", "belt", "bag", "insole", "outsole",
		},
	},
}

// hardcodedFallbackExclusions mirrors the exact fallback list baked into
// realift-sdk.liquid, used only as a secondary safety net if the debug
// context doesn't tell us whether the exclude list is store-customized.
var hardcodedFallbackExclusions = map[string]bool{
	"wallet": true, "belt": true, "sock": true, "insole": true, "outsole": true,
	"card": true, "bag": true, "backpack": true, "purse": true, "accessor": true,
}

type relevanceGuess int

const (
	guessUnknown relevanceGuess = iota
	guessRelevant
	guessIrrelevant
)

func normalizeAppType(a string) string {
	return strings.ToLower(strings.TrimSpace(a))
}

// isFootApp reports whether appType is one of the two footwear-oriented
// apps, for which the hardcoded fallback exclusion list (socks, insoles,
// outsoles, ...) is known-correct rather than merely advisory.
func isFootApp(appType string) bool {
	switch normalizeAppType(appType) {
	case "realfoot", "foot", "foot3d":
		return true
	default:
		return false
	}
}

// guessAppRelevance returns a tri-state guess plus the hint keyword that
// drove it, based on product title/type text. Irrelevant hints take
// precedence over relevant ones (an "irrelevant" hit is a stronger signal:
// e.g. a title containing both "shoe" and "sock" — a shoe-shaped sock —
// is rare enough that erring toward irrelevant is safer for a footwear app).
func guessAppRelevance(appType, title, productType string) (relevanceGuess, string) {
	hints, ok := appHintTable[normalizeAppType(appType)]
	if !ok {
		return guessUnknown, ""
	}
	text := strings.ToLower(title + " " + productType)
	for _, kw := range hints.irrelevant {
		if strings.Contains(text, kw) {
			return guessIrrelevant, kw
		}
	}
	for _, kw := range hints.relevant {
		if strings.Contains(text, kw) {
			return guessRelevant, kw
		}
	}
	return guessUnknown, ""
}
