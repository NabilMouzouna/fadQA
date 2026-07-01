package enumerate

import "time"

// Product is one discovered, published product on the store.
type Product struct {
	Handle      string
	Title       string
	ProductType string
	Tags        []string
	PublishedAt time.Time
	URL         string
	Source      string // "products.json" | "collections_all" | "sitemap"
}

// EnumResult is the outcome of enumerating a store's products.
type EnumResult struct {
	Products      []Product
	Method        string
	IsShopify     bool
	PasswordLock  bool
	CanonicalHost string
	Warnings      []string
}
