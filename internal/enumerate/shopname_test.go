package enumerate

import "testing"

func TestExtractShopName_OGSiteName(t *testing.T) {
	body := []byte(`<html><head><meta property="og:site_name" content="Acme &amp; Co"><title>Home | Acme</title></head></html>`)
	if got := extractShopName(body, "https://acme.myshopify.com"); got != "Acme & Co" {
		t.Fatalf("expected og:site_name, got %q", got)
	}
}

func TestExtractShopName_TitleFallback(t *testing.T) {
	body := []byte(`<html><head><title>Rudis Wrestling – Premium Gear</title></head></html>`)
	got := extractShopName(body, "https://x.myshopify.com")
	if got != "Rudis Wrestling" {
		t.Fatalf("expected title's name segment, got %q", got)
	}
}

func TestExtractShopName_HostFallback(t *testing.T) {
	cases := map[string]string{
		"https://acme.myshopify.com":            "Acme",
		"https://shop.crosskix.com":             "Crosskix",
		"https://abc123-999.shopifypreview.com": "abc123-999.shopifypreview.com",
	}
	for host, want := range cases {
		if got := extractShopName([]byte("<html></html>"), host); got != want {
			t.Errorf("hostName(%q)=%q want %q", host, got, want)
		}
	}
}
