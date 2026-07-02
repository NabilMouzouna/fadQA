package enumerate

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	ogSiteNameRe = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:site_name["'][^>]*>`)
	contentRe    = regexp.MustCompile(`(?is)content=["']([^"']*)["']`)
	titleRe      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// extractShopName lifts a friendly store name from the homepage HTML,
// preferring the explicit og:site_name meta tag, then the <title> (cleaned
// of a trailing tagline), and finally falling back to a name derived from
// the host. Best-effort only — used for report/Slack display, never for
// logic.
func extractShopName(body []byte, canonicalHost string) string {
	if name := ogSiteName(body); name != "" {
		return name
	}
	if name := titleName(body); name != "" {
		return name
	}
	return hostName(canonicalHost)
}

func ogSiteName(body []byte) string {
	tag := ogSiteNameRe.Find(body)
	if tag == nil {
		return ""
	}
	m := contentRe.FindSubmatch(tag)
	if m == nil {
		return ""
	}
	return cleanText(string(m[1]))
}

func titleName(body []byte) string {
	m := titleRe.FindSubmatch(body)
	if m == nil {
		return ""
	}
	t := cleanText(string(m[1]))
	// Homepage titles are commonly "Shop Name – tagline"; take the first
	// segment before a separator. This is a fuzzy fallback — og:site_name is
	// the reliable source and is tried first.
	if i := strings.IndexAny(t, "–—|·"); i != -1 {
		if first := strings.TrimSpace(t[:i]); first != "" {
			return first
		}
	}
	if i := strings.Index(t, " - "); i != -1 {
		if first := strings.TrimSpace(t[:i]); first != "" {
			return first
		}
	}
	return t
}

// hostName derives a name from the host: drops www./scheme, strips the
// Shopify suffix for *.myshopify.com, and title-cases the label. Preview
// domains (random-id.shopifypreview.com) have no meaningful name, so the
// bare host is returned.
func hostName(canonical string) string {
	host := canonical
	if u, err := url.Parse(canonical); err == nil && u.Host != "" {
		host = u.Host
	}
	host = strings.TrimPrefix(host, "www.")

	switch {
	case strings.HasSuffix(host, ".shopifypreview.com"):
		return host // random id, not a friendly name
	case strings.HasSuffix(host, ".myshopify.com"):
		return titleCase(strings.TrimSuffix(host, ".myshopify.com"))
	}
	labels := strings.Split(host, ".")
	if len(labels) >= 2 {
		return titleCase(labels[len(labels)-2])
	}
	return host
}

func titleCase(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	fields := strings.Fields(s)
	for i, f := range fields {
		if f == "" {
			continue
		}
		fields[i] = strings.ToUpper(f[:1]) + f[1:]
	}
	return strings.Join(fields, " ")
}

func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace/newlines
	return strings.TrimSpace(s)
}
