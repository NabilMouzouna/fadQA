package verdict

import (
	"bytes"
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

const (
	configTagID = "realift-config"
	debugTagID  = "realift-debug-context"
	buttonTag   = "realift-button"
)

// LooksLikeSDKPresent is a cheap pre-filter: if the raw bytes don't even
// contain the config tag id, there is no point tokenizing the whole page.
func LooksLikeSDKPresent(body []byte) bool {
	return bytes.Contains(body, []byte(configTagID))
}

// Extract streams the HTML body and pulls out the three realift signals
// without building a full DOM tree, keeping memory low.
func Extract(body []byte) Extracted {
	var out Extracted

	if !LooksLikeSDKPresent(body) {
		return out
	}

	var configRaw, debugRaw string

	z := html.NewTokenizer(bytes.NewReader(body))
	// pendingID tracks which script tag's text we should capture next.
	pendingID := ""

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)

			if tag == buttonTag {
				out.HasButton = true
			}

			if tag == "script" && hasAttr {
				id := attrValue(z, "id")
				switch id {
				case configTagID:
					out.HasConfigTag = true
					pendingID = configTagID
				case debugTagID:
					out.HasDebugTag = true
					pendingID = debugTagID
				default:
					pendingID = ""
				}
			}

		case html.TextToken:
			if pendingID != "" {
				text := string(z.Text())
				switch pendingID {
				case configTagID:
					configRaw += text
				case debugTagID:
					debugRaw += text
				}
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == "script" {
				pendingID = ""
			}
		}
	}

	if out.HasConfigTag {
		cfg, truncated := parseConfig(configRaw)
		out.Config = cfg
		if truncated {
			out.Truncated = true
		}
	}
	if out.HasDebugTag {
		dbg, truncated := parseDebug(debugRaw)
		out.Debug = dbg
		if truncated {
			out.Truncated = true
		}
	}

	return out
}

// attrValue reads a single attribute by name from the tokenizer's current
// start tag, consuming all attributes in the process (matches x/net/html's
// TagAttr contract: must call it in a loop until hasAttr is false).
func attrValue(z *html.Tokenizer, name string) string {
	var val string
	for {
		key, v, more := z.TagAttr()
		if string(key) == name {
			val = string(v)
		}
		if !more {
			break
		}
	}
	return val
}

func parseConfig(raw string) (*Config, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, true
	}
	return &cfg, false
}

func parseDebug(raw string) (*DebugContext, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	var dbg DebugContext
	if err := json.Unmarshal([]byte(raw), &dbg); err != nil {
		return nil, true
	}
	return &dbg, false
}

// IsEmptyJSON reports whether a json.RawMessage represents "no value" for our
// purposes: nil, null, "", {}, [], or whitespace-only.
func IsEmptyJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "", "null", `""`, "{}", "[]":
		return true
	default:
		return false
	}
}
