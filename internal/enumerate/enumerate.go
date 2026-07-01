package enumerate

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/realift/fad-qa/internal/fetch"
)

// Enumerator discovers a Shopify store's published products via public,
// unauthenticated endpoints only.
type Enumerator struct {
	Client  *http.Client
	Limiter *fetch.Limiter
}

func New(client *http.Client, limiter *fetch.Limiter) *Enumerator {
	return &Enumerator{Client: client, Limiter: limiter}
}

// Enumerate normalizes the store URL, detects whether it's a reachable
// Shopify storefront, then tries /products.json, /collections/all, and
// sitemap.xml in that order, stopping at the first strategy that yields
// results.
func (e *Enumerator) Enumerate(ctx context.Context, rawStoreURL string) (EnumResult, error) {
	base, err := normalizeBase(rawStoreURL)
	if err != nil {
		return EnumResult{}, err
	}

	canonical, isShopify, passwordLock, warnings, err := e.detect(ctx, base)
	if err != nil {
		return EnumResult{}, err
	}

	res := EnumResult{
		IsShopify:     isShopify,
		PasswordLock:  passwordLock,
		CanonicalHost: canonical,
		Warnings:      warnings,
	}

	if !isShopify {
		res.Warnings = append(res.Warnings, "Domain does not appear to be a Shopify storefront (no Shopify markers found in headers, cookies, or page body).")
		return res, nil
	}
	if passwordLock {
		res.Warnings = append(res.Warnings, "Storefront is password-protected; cannot enumerate or test products without access.")
		return res, nil
	}

	if products, ok := e.tryProductsJSON(ctx, canonical, "/products.json", "products.json"); ok && len(products) > 0 {
		res.Products = products
		res.Method = "products.json"
		return res, nil
	}

	if products, ok := e.tryProductsJSON(ctx, canonical, "/collections/all/products.json", "collections_all"); ok && len(products) > 0 {
		res.Products = products
		res.Method = "collections_all"
		return res, nil
	}

	if products, ok := e.trySitemap(ctx, canonical); ok && len(products) > 0 {
		res.Products = products
		res.Method = "sitemap"
		return res, nil
	}

	res.Warnings = append(res.Warnings, "No products discoverable via /products.json, /collections/all/products.json, or sitemap.xml.")
	return res, nil
}

// normalizeBase defaults to https when no scheme is given, strips
// path/query/fragment, and validates the input has a usable host. An
// explicit http:// is left as-is rather than force-upgraded — real Shopify
// stores are always https, but there's no reason to override a caller who
// deliberately points at a plain-http endpoint (e.g. local testing).
func normalizeBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("store URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid store URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid store URL %q: missing host", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid store URL %q: unsupported scheme %q", raw, u.Scheme)
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

var shopifyBodyMarkers = [][]byte{
	[]byte("cdn.shopify.com"),
	[]byte("Shopify.theme"),
	[]byte("window.Shopify"),
	[]byte("/cdn/shop/"),
	[]byte("shopify-features"),
}

// detect issues one request to the store root, follows redirects, and
// classifies the result as Shopify/not, password-locked/not, adopting the
// final (post-redirect) host as canonical for all subsequent requests.
func (e *Enumerator) detect(ctx context.Context, base string) (canonical string, isShopify bool, passwordLock bool, warnings []string, err error) {
	result, ferr := fetch.GetPage(ctx, e.Client, e.Limiter, base+"/")
	if ferr != nil {
		return base, false, false, nil, fmt.Errorf("could not reach %s: %w", base, ferr)
	}

	canonical = base
	if result.FinalURL != "" {
		if u, perr := url.Parse(result.FinalURL); perr == nil && u.Host != "" {
			canonical = u.Scheme + "://" + u.Host
		}
	}

	isShopify = isShopifyResponse(result)
	passwordLock = isPasswordLocked(result, canonical)

	if !isShopify {
		// One more chance: password-protected stores sometimes fail the
		// marker scan because the body is just the password page. If we
		// clearly detect the password wall, treat it as Shopify+locked
		// rather than "not Shopify".
		if passwordLock {
			isShopify = true
		}
	}

	return canonical, isShopify, passwordLock, warnings, nil
}

func isShopifyResponse(r fetch.Result) bool {
	if r.Header != nil {
		if r.Header.Get("x-shopify-stage") != "" {
			return true
		}
		if strings.Contains(strings.ToLower(r.Header.Get("powered-by")), "shopify") {
			return true
		}
		for _, c := range r.Header.Values("Set-Cookie") {
			lc := strings.ToLower(c)
			if strings.Contains(lc, "_shopify_") || strings.Contains(lc, "_secure_session_id") {
				return true
			}
		}
	}
	for _, marker := range shopifyBodyMarkers {
		if bytes.Contains(r.Body, marker) {
			return true
		}
	}
	return false
}

func isPasswordLocked(r fetch.Result, canonical string) bool {
	if r.StatusCode == http.StatusUnauthorized || r.StatusCode == http.StatusForbidden {
		return true
	}
	if strings.Contains(r.FinalURL, "/password") {
		return true
	}
	if bytes.Contains(r.Body, []byte("template-password")) {
		return true
	}
	if bytes.Contains(r.Body, []byte(`id="login_form"`)) && bytes.Contains(r.Body, []byte("storefront_password")) {
		return true
	}
	return false
}
