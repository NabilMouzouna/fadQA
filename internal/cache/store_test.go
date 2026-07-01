package cache

import (
	"os"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)

	c := New("example.myshopify.com", "realfoot")
	c.Upsert("blue-shoe", "Blue Shoe", "https://example.myshopify.com/products/blue-shoe", "PASS", "ok", now)
	c.Upsert("wool-sock", "Wool Sock", "https://example.myshopify.com/products/wool-sock", "SKIP_NOT_RELEVANT", "irrelevant", now)
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, ok := Load(dir, "example.myshopify.com", "realfoot")
	if !ok {
		t.Fatalf("expected cache to load")
	}
	if len(loaded.Products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(loaded.Products))
	}
	if loaded.Products["blue-shoe"].LastVerdict != "PASS" {
		t.Fatalf("unexpected verdict: %+v", loaded.Products["blue-shoe"])
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Load(dir, "nope.myshopify.com", "realfoot"); ok {
		t.Fatalf("expected ok=false for missing cache")
	}
}

func TestLoad_CorruptFileBackedUp(t *testing.T) {
	dir := t.TempDir()
	c := New("example.myshopify.com", "realfoot")
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	p := filePath(dir, "example.myshopify.com", "realfoot")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt write: %v", err)
	}

	if _, ok := Load(dir, "example.myshopify.com", "realfoot"); ok {
		t.Fatalf("expected ok=false for corrupt cache")
	}
	if _, err := os.Stat(p + ".bad"); err != nil {
		t.Fatalf("expected corrupt file backed up to .bad: %v", err)
	}
}

func TestLoad_SchemaVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	c := New("example.myshopify.com", "realfoot")
	c.SchemaVersion = 999
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := Load(dir, "example.myshopify.com", "realfoot"); ok {
		t.Fatalf("expected ok=false for schema mismatch")
	}
}

func TestFailingHandles(t *testing.T) {
	c := New("s", "realfoot")
	now := time.Now()
	c.Upsert("a", "A", "url-a", "PASS", "", now)
	c.Upsert("b", "B", "url-b", "FAIL_NOT_INCLUDED", "", now)
	c.Upsert("c", "C", "url-c", "ERROR", "", now)

	withoutErrors := c.FailingHandles(false)
	if len(withoutErrors) != 1 || withoutErrors[0] != "b" {
		t.Fatalf("expected only 'b', got %v", withoutErrors)
	}

	withErrors := c.FailingHandles(true)
	if len(withErrors) != 2 {
		t.Fatalf("expected 2 handles including error, got %v", withErrors)
	}
}

func TestUpsert_TracksFirstSeenAndLastChanged(t *testing.T) {
	c := New("s", "realfoot")
	t0 := time.Now()
	c.Upsert("a", "A", "url", "FAIL_NOT_INCLUDED", "r1", t0)

	t1 := t0.Add(time.Hour)
	c.Upsert("a", "A", "url", "FAIL_NOT_INCLUDED", "r1", t1) // same verdict, shouldn't bump LastChanged
	if c.Products["a"].LastChanged != t0 {
		t.Fatalf("expected LastChanged to stay at t0 when verdict unchanged, got %v", c.Products["a"].LastChanged)
	}

	t2 := t1.Add(time.Hour)
	c.Upsert("a", "A", "url", "PASS", "fixed", t2) // verdict flips
	if c.Products["a"].LastChanged != t2 {
		t.Fatalf("expected LastChanged to update to t2 on verdict flip, got %v", c.Products["a"].LastChanged)
	}
	if c.Products["a"].FirstSeen != t0 {
		t.Fatalf("expected FirstSeen to remain t0, got %v", c.Products["a"].FirstSeen)
	}
}

func TestDetectNew(t *testing.T) {
	c := New("s", "realfoot")
	now := time.Now()
	c.Upsert("a", "A", "url", "PASS", "", now)
	c.Upsert("b", "B", "url", "PASS", "", now)

	newH, goneH := c.DetectNew([]string{"a", "c"})
	if len(newH) != 1 || newH[0] != "c" {
		t.Fatalf("expected new=[c], got %v", newH)
	}
	if len(goneH) != 1 || goneH[0] != "b" {
		t.Fatalf("expected gone=[b], got %v", goneH)
	}
}

func TestKeywordsChanged(t *testing.T) {
	c := New("s", "realfoot")
	c.RealiftKeywords = []string{"shoe", "boot"}
	c.ExcludedKeywords = []string{"sock"}

	if c.KeywordsChanged([]string{"boot", "shoe"}, []string{"sock"}) {
		t.Fatalf("expected no change for same sets in different order")
	}
	if !c.KeywordsChanged([]string{"shoe"}, []string{"sock"}) {
		t.Fatalf("expected change detected when include list shrinks")
	}
}
