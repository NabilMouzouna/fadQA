package enumerate

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/realift/fad-qa/internal/fetch"
)

var productSitemapPattern = regexp.MustCompile(`sitemap_products_\d+\.xml`)

type sitemapIndex struct {
	XMLName  xml.Name       `xml:"sitemapindex"`
	Sitemaps []sitemapEntry `xml:"sitemap"`
}

type sitemapEntry struct {
	Loc string `xml:"loc"`
}

// trySitemap fetches sitemap.xml, selects sitemap_products_*.xml entries
// (or, for small stores, treats sitemap.xml itself as a flat urlset), and
// stream-parses for product page URLs. Used only when /products.json and
// /collections/all/products.json are both empty/disabled.
func (e *Enumerator) trySitemap(ctx context.Context, canonical string) ([]Product, bool) {
	indexURL := canonical + "/sitemap.xml"
	result, err := fetch.GetPage(ctx, e.Client, e.Limiter, indexURL)
	if err != nil || result.StatusCode != 200 {
		return nil, false
	}

	seen := map[string]Product{}

	var idx sitemapIndex
	hasIndex := xml.Unmarshal(result.Body, &idx) == nil && len(idx.Sitemaps) > 0

	if hasIndex {
		for _, s := range idx.Sitemaps {
			if !productSitemapPattern.MatchString(s.Loc) {
				continue
			}
			sub, ferr := fetch.GetPage(ctx, e.Client, e.Limiter, s.Loc)
			if ferr != nil || sub.StatusCode != 200 {
				continue
			}
			parseURLSet(sub.Body, canonical, seen)
		}
	} else {
		parseURLSet(result.Body, canonical, seen)
	}

	if len(seen) == 0 {
		return nil, false
	}
	out := make([]Product, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return out, true
}

// parseURLSet streams a <urlset>'s <url><loc>/<lastmod> entries via a
// token-by-token decoder (bounded memory even for multi-MB sitemaps),
// keeping only entries that look like product page URLs.
func parseURLSet(body []byte, canonical string, seen map[string]Product) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var currentLoc, currentLastmod string
	inURL := false

	for {
		tok, terr := decoder.Token()
		if terr != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "url":
				inURL = true
				currentLoc, currentLastmod = "", ""
			case "loc":
				if inURL {
					var text string
					_ = decoder.DecodeElement(&text, &t)
					currentLoc = text
				}
			case "lastmod":
				if inURL {
					var text string
					_ = decoder.DecodeElement(&text, &t)
					currentLastmod = text
				}
			}
		case xml.EndElement:
			if t.Name.Local != "url" {
				continue
			}
			inURL = false
			handle := extractHandle(currentLoc)
			if handle == "" {
				continue
			}
			if _, exists := seen[handle]; exists {
				continue
			}
			p := Product{
				Handle: handle,
				URL:    fmt.Sprintf("%s/products/%s", canonical, handle),
				Source: "sitemap",
			}
			if currentLastmod != "" {
				if lt, perr := time.Parse(time.RFC3339, currentLastmod); perr == nil {
					p.PublishedAt = lt
				}
			}
			seen[handle] = p
		}
	}
}

func extractHandle(productURL string) string {
	idx := strings.Index(productURL, "/products/")
	if idx == -1 {
		return ""
	}
	handle := productURL[idx+len("/products/"):]
	if q := strings.IndexAny(handle, "?#"); q != -1 {
		handle = handle[:q]
	}
	return strings.Trim(handle, "/")
}
