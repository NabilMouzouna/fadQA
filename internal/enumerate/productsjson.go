package enumerate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/realift/fad-qa/internal/fetch"
)

const (
	productsPerPage = 250 // Shopify clamps /products.json limit at 250
	maxPages        = 400 // safety cap: 400*250 = 100,000 products
)

type rawProductsResponse struct {
	Products []rawProduct `json:"products"`
}

type rawProduct struct {
	Title       string          `json:"title"`
	Handle      string          `json:"handle"`
	ProductType string          `json:"product_type"`
	PublishedAt string          `json:"published_at"`
	Tags        json.RawMessage `json:"tags"`
}

// normalizeTags handles Shopify's inconsistent tags encoding: /products.json
// returns an array, but /collections/all/products.json can return a
// comma-joined string.
func normalizeTags(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var asArray []string
	if err := json.Unmarshal(raw, &asArray); err == nil {
		return asArray
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			return nil
		}
		parts := strings.Split(asString, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

// tryProductsJSON paginates a Shopify public products-listing JSON endpoint
// (/products.json or /collections/all/products.json — same response shape)
// until a short/empty page or a definitive non-200 is hit.
func (e *Enumerator) tryProductsJSON(ctx context.Context, canonical, path, source string) ([]Product, bool) {
	seen := map[string]Product{}

	for page := 1; page <= maxPages; page++ {
		pageURL := fmt.Sprintf("%s%s?limit=%d&page=%d", canonical, path, productsPerPage, page)
		result, err := fetch.GetPage(ctx, e.Client, e.Limiter, pageURL)
		if err != nil || result.StatusCode != 200 {
			break
		}

		var parsed rawProductsResponse
		if jsonErr := json.Unmarshal(result.Body, &parsed); jsonErr != nil {
			break
		}
		if len(parsed.Products) == 0 {
			break
		}

		for _, rp := range parsed.Products {
			if rp.Handle == "" {
				continue
			}
			p := Product{
				Handle:      rp.Handle,
				Title:       rp.Title,
				ProductType: rp.ProductType,
				Tags:        normalizeTags(rp.Tags),
				URL:         fmt.Sprintf("%s/products/%s", canonical, rp.Handle),
				Source:      source,
			}
			if rp.PublishedAt != "" {
				if t, perr := time.Parse(time.RFC3339, rp.PublishedAt); perr == nil {
					p.PublishedAt = t
				}
			}
			seen[p.Handle] = mergeProduct(seen[p.Handle], p)
		}

		if len(parsed.Products) < productsPerPage {
			break
		}
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

// mergeProduct combines a duplicate handle sighting, preferring existing
// non-empty fields but filling in any gaps from the new sighting.
func mergeProduct(existing, incoming Product) Product {
	if existing.Handle == "" {
		return incoming
	}
	if existing.Title == "" {
		existing.Title = incoming.Title
	}
	if existing.ProductType == "" {
		existing.ProductType = incoming.ProductType
	}
	if len(existing.Tags) == 0 {
		existing.Tags = incoming.Tags
	}
	return existing
}
