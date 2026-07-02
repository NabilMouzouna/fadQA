package enumerate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/realift/fad-qa/internal/fetch"
)

func newTestEnumerator() *Enumerator {
	return New(fetch.NewClient(), fetch.NewLimiter(8, 1000))
}

func TestNormalizeTags(t *testing.T) {
	arr := normalizeTags(json.RawMessage(`["a","b"]`))
	if len(arr) != 2 || arr[0] != "a" {
		t.Fatalf("unexpected array tags: %v", arr)
	}
	str := normalizeTags(json.RawMessage(`"a, b,  c"`))
	if len(str) != 3 || str[2] != "c" {
		t.Fatalf("unexpected string tags: %v", str)
	}
	empty := normalizeTags(json.RawMessage(`""`))
	if len(empty) != 0 {
		t.Fatalf("expected empty for blank string, got %v", empty)
	}
}

func TestNormalizeBase(t *testing.T) {
	cases := map[string]string{
		"example.myshopify.com":             "https://example.myshopify.com",
		"http://example.com/":               "http://example.com",
		"https://example.com/some/path?x=1": "https://example.com",
	}
	for input, want := range cases {
		got, err := normalizeBase(input)
		if err != nil {
			t.Fatalf("normalizeBase(%q) error: %v", input, err)
		}
		if got != want {
			t.Errorf("normalizeBase(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeBase(""); err == nil {
		t.Fatalf("expected error for empty input")
	}
	if _, err := normalizeBase("ftp://example.com"); err == nil {
		t.Fatalf("expected error for unsupported scheme")
	}
}

func TestExtractHandle(t *testing.T) {
	cases := map[string]string{
		"https://store.com/products/blue-shoe":           "blue-shoe",
		"https://store.com/products/blue-shoe?variant=1": "blue-shoe",
		"https://store.com/collections/all":              "",
	}
	for input, want := range cases {
		if got := extractHandle(input); got != want {
			t.Errorf("extractHandle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTryProductsJSON_PaginationAndDedupe(t *testing.T) {
	page1 := rawProductsResponse{Products: []rawProduct{
		{Handle: "a", Title: "Shoe A", Tags: json.RawMessage(`["x","y"]`)},
	}}
	for len(page1.Products) < productsPerPage {
		page1.Products = append(page1.Products, rawProduct{Handle: fmt.Sprintf("pad-%d", len(page1.Products)), Title: "Pad"})
	}
	page2 := rawProductsResponse{Products: []rawProduct{
		{Handle: "b", Title: "Shoe B", Tags: json.RawMessage(`"tag1, tag2"`)},
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_ = json.NewEncoder(w).Encode(page1)
		case "2":
			_ = json.NewEncoder(w).Encode(page2)
		default:
			_ = json.NewEncoder(w).Encode(rawProductsResponse{})
		}
	}))
	defer srv.Close()

	e := newTestEnumerator()
	products, ok := e.tryProductsJSON(context.Background(), srv.URL, "/products.json", "products.json")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if len(products) != productsPerPage+1 {
		t.Fatalf("expected %d products (full page1 + 1 from page2), got %d", productsPerPage+1, len(products))
	}

	var shoeB *Product
	for i := range products {
		if products[i].Handle == "b" {
			shoeB = &products[i]
		}
	}
	if shoeB == nil {
		t.Fatalf("expected handle 'b' present")
	}
	if len(shoeB.Tags) != 2 || shoeB.Tags[0] != "tag1" {
		t.Fatalf("expected comma-string tags normalized, got %v", shoeB.Tags)
	}
}

func TestTryProductsJSON_StopsOnShortPage(t *testing.T) {
	// A page shorter than productsPerPage is itself the stop signal — no
	// extra request should be made to confirm the next page is empty.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rawProductsResponse{Products: []rawProduct{{Handle: "only-one"}}})
	}))
	defer srv.Close()

	e := newTestEnumerator()
	products, ok := e.tryProductsJSON(context.Background(), srv.URL, "/products.json", "products.json")
	if !ok || len(products) != 1 {
		t.Fatalf("expected exactly 1 product, got %d (ok=%v)", len(products), ok)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 request for a short page, got %d", calls)
	}
}

func TestTryProductsJSON_StopsOnEmptyFollowupPage(t *testing.T) {
	// A full page must trigger a follow-up request, which then stops on
	// the empty page.
	calls := 0
	full := rawProductsResponse{}
	for len(full.Products) < productsPerPage {
		full.Products = append(full.Products, rawProduct{Handle: fmt.Sprintf("p-%d", len(full.Products))})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_ = json.NewEncoder(w).Encode(full)
			return
		}
		_ = json.NewEncoder(w).Encode(rawProductsResponse{})
	}))
	defer srv.Close()

	e := newTestEnumerator()
	products, ok := e.tryProductsJSON(context.Background(), srv.URL, "/products.json", "products.json")
	if !ok || len(products) != productsPerPage {
		t.Fatalf("expected %d products, got %d (ok=%v)", productsPerPage, len(products), ok)
	}
	if calls != 2 {
		t.Fatalf("expected exactly 2 requests (full page1, empty page2), got %d", calls)
	}
}

func TestTrySitemap(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><sitemapindex><sitemap><loc>%s/sitemap_products_1.xml</loc></sitemap></sitemapindex>`, srv.URL)
	})
	mux.HandleFunc("/sitemap_products_1.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?><urlset>
			<url><loc>http://x/products/blue-shoe</loc><lastmod>2024-01-01T00:00:00Z</lastmod></url>
			<url><loc>http://x/collections/all</loc></url>
		</urlset>`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	e := newTestEnumerator()
	products, ok := e.trySitemap(context.Background(), srv.URL)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if len(products) != 1 || products[0].Handle != "blue-shoe" {
		t.Fatalf("unexpected products: %+v", products)
	}
}

func TestDetect_ShopifyMarkerAndPasswordLock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Shopify-Stage", "production")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<html><body class="template-password">storefront_password</body></html>`))
	}))
	defer srv.Close()

	e := newTestEnumerator()
	canonical, isShopify, passwordLock, _, _, err := e.detect(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isShopify {
		t.Fatalf("expected isShopify=true")
	}
	if !passwordLock {
		t.Fatalf("expected passwordLock=true")
	}
	if canonical != srv.URL {
		t.Fatalf("expected canonical=%q, got %q", srv.URL, canonical)
	}
}

func TestDetect_NotShopify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>Just a regular website.</body></html>`))
	}))
	defer srv.Close()

	e := newTestEnumerator()
	_, isShopify, passwordLock, _, _, err := e.detect(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isShopify {
		t.Fatalf("expected isShopify=false")
	}
	if passwordLock {
		t.Fatalf("expected passwordLock=false")
	}
}
