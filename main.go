// Command fad-qa crawls a Shopify store's product pages over plain HTTP and
// reports whether the Realift size-measurement button shows up correctly,
// and if not, exactly why — no headless browser, no AI. The show/hide
// decision is fully determined by signals rendered server-side into every
// product page's raw HTML (see internal/verdict).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/realift/fad-qa/internal/cache"
	"github.com/realift/fad-qa/internal/enumerate"
	"github.com/realift/fad-qa/internal/fetch"
	"github.com/realift/fad-qa/internal/notify"
	"github.com/realift/fad-qa/internal/pool"
	"github.com/realift/fad-qa/internal/report"
	"github.com/realift/fad-qa/internal/slack"
	"github.com/realift/fad-qa/internal/ui"
	"github.com/realift/fad-qa/internal/verdict"
)

const banner = "\x1b[38;5;177m" + `
 ________   ________  ________  ________   ________
|\  _____\ |\   __  \|\   ___ \|\   __  \ |\   __  \
\ \  \__/  \ \  \|\  \ \  \_|\ \ \  \|\  \\ \  \|\  \
 \ \   __\  \ \   __  \ \  \ \\ \ \  \\\  \\ \   __  \
  \ \  \_|   \ \  \ \  \ \  \_\\ \ \  \\\  \\ \  \ \  \
   \ \__\     \ \__\ \__\ \_______\ \_____  \\ \__\ \__\
    \|__|      \|__|\|__|\|_______|\|___| \__\\|__|\|__|
                                           \__\

                 If Fadoua Were a Tool...
                    It Would Be FadQA.
                    Powered by Realift.
` + "\x1b[0m\n"

var validAppTypes = map[string]bool{
	"realfoot": true, "realhand": true, "realbody": true, "foot3d": true,
}

type options struct {
	store       string
	appType     string
	mode        string
	workers     int
	rate        float64
	outDir      string
	cacheDir    string
	noSound     bool
	noNotify    bool
	noKeepAwake bool
	verbose     bool
}

func main() {
	opts := parseFlags()

	fmt.Print(banner)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !opts.noKeepAwake {
		stopAwake := notify.StartKeepAwake()
		defer stopAwake()
	}

	if err := run(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// defaultBaseDir resolves to the directory containing the running
// executable (following symlinks, so a Homebrew-style symlinked install
// still resolves to the real binary's location), not the current working
// directory. This is what lets fad-qa self-create its cache/ and reports/
// folders next to itself no matter where or how it's launched — a
// double-clicked exe, a shortcut, or a terminal opened in an unrelated
// directory — which matters once the binary is handed to teammates who
// don't have the repo checked out at all. Falls back to "." if the OS
// can't report the executable path (rare).
func defaultBaseDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func parseFlags() options {
	base := defaultBaseDir()
	defaultOut := filepath.Join(base, "reports")
	defaultCache := filepath.Join(base, "cache")

	var o options
	flag.StringVar(&o.store, "store", "", "Shopify store URL to test (required)")
	flag.StringVar(&o.appType, "app", "", "app type: realfoot | realhand | realbody | foot3d (required)")
	flag.StringVar(&o.mode, "mode", "full", "test mode: full | quick (quick retests only previously-failing products)")
	flag.IntVar(&o.workers, "workers", 4, "max concurrent requests, 1-32 (the adaptive limiter starts lower and ramps up to this ceiling)")
	flag.Float64Var(&o.rate, "rate", 4, "max steady-state requests per second (ramped up to, not started at)")
	flag.StringVar(&o.outDir, "out", defaultOut, "directory to write the Markdown report to (default: next to the executable)")
	flag.StringVar(&o.cacheDir, "cache", defaultCache, "directory holding per-store cache files (default: next to the executable)")
	flag.BoolVar(&o.noSound, "no-sound", false, "disable completion sound")
	flag.BoolVar(&o.noNotify, "no-notify", false, "disable desktop notification")
	flag.BoolVar(&o.noKeepAwake, "no-keepawake", false, "don't prevent the machine from sleeping during the run")
	flag.BoolVar(&o.verbose, "verbose", false, "print per-product progress")
	flag.Parse()

	if o.store == "" {
		fmt.Fprintln(os.Stderr, "error: --store is required")
		flag.Usage()
		os.Exit(2)
	}
	o.appType = strings.ToLower(strings.TrimSpace(o.appType))
	if !validAppTypes[o.appType] {
		fmt.Fprintln(os.Stderr, "error: --app must be one of: realfoot, realhand, realbody, foot3d")
		flag.Usage()
		os.Exit(2)
	}
	o.mode = strings.ToLower(strings.TrimSpace(o.mode))
	if o.mode != "full" && o.mode != "quick" {
		fmt.Fprintln(os.Stderr, "error: --mode must be full or quick")
		flag.Usage()
		os.Exit(2)
	}
	if o.workers < 1 {
		o.workers = 1
	}
	if o.workers > 32 {
		o.workers = 32
	}
	return o
}

func run(ctx context.Context, opts options) error {
	client := fetch.NewClient()
	limiter := fetch.NewLimiter(opts.workers, opts.rate)
	enumerator := enumerate.New(client, limiter)

	// bar is created later (only for the testing phase on a TTY). The cooldown
	// hook captures it by reference so a Cloudflare pause is surfaced cleanly
	// whether it happens during enumeration (bar still nil → plain warn) or
	// mid-progress-bar (bar set → Note clears and reprints around it).
	var bar *ui.ProductBar
	limiter.SetCooldownHook(func(d time.Duration, episode int) {
		msg := fmt.Sprintf("Store is rate-limiting us (Cloudflare bot protection) — pausing %s to let it clear [cooldown #%d], then resuming slower.", ui.FormatDuration(d), episode)
		if bar != nil {
			bar.Note("    " + ui.Yellow("[waiting] ") + msg)
		} else {
			ui.Warn("%s", msg)
		}
	})

	ui.Section("Configuration")
	ui.KV("Store", opts.store)
	ui.KV("App type", opts.appType)
	ui.KV("Mode", opts.mode)
	ui.KV("Max workers", fmt.Sprintf("%d", opts.workers))
	ui.KV("Max rate", fmt.Sprintf("%.0f req/s", opts.rate))

	ui.Step(1, 3, "Enumerating store")
	enumResult, err := enumerator.Enumerate(ctx, opts.store)
	if err != nil {
		return fmt.Errorf("enumerate: %w", err)
	}

	now := time.Now()

	if !enumResult.IsShopify || enumResult.PasswordLock {
		return writeAbortReport(ctx, client, opts, enumResult, now)
	}
	if len(enumResult.Products) == 0 {
		return writeAbortReport(ctx, client, opts, enumResult, now)
	}
	ui.Success("Shopify store confirmed")
	ui.Success("Discovered %d products via %s", len(enumResult.Products), enumResult.Method)

	canonical := enumResult.CanonicalHost
	if canonical == "" {
		canonical = opts.store
	}

	cacheStore, hadCache := cache.Load(opts.cacheDir, canonical, opts.appType)
	mode := opts.mode
	if mode == "quick" && !hadCache {
		ui.Warn("No previous cache found for this store — running full instead.")
		mode = "full"
	}
	if cacheStore == nil {
		cacheStore = cache.New(canonical, opts.appType)
	}

	currentHandles := make([]string, 0, len(enumResult.Products))
	byHandle := make(map[string]enumerate.Product, len(enumResult.Products))
	for _, p := range enumResult.Products {
		currentHandles = append(currentHandles, p.Handle)
		byHandle[p.Handle] = p
	}
	newHandles, goneHandles := cacheStore.DetectNew(currentHandles)

	var jobs []enumerate.Product
	if mode == "full" {
		jobs = enumResult.Products
	} else {
		failing := cacheStore.FailingHandles(true)
		for _, h := range failing {
			if p, ok := byHandle[h]; ok {
				jobs = append(jobs, p)
			}
		}
		ui.Info("Quick mode: retesting %d previously-failing/errored products", len(jobs))
		if len(newHandles) > 0 {
			ui.Warn("%d new products since the last run were not tested (run --mode full to include them)", len(newHandles))
		}
	}

	total := len(jobs)

	ui.Section("Test Run")
	ui.KV("Store", canonical)
	ui.KV("Products", fmt.Sprintf("%d", total))

	ui.Step(2, 3, "Testing products")

	if ui.IsTTY() && !opts.verbose && total > 0 {
		bar = ui.NewProductBar(total)
	}

	tested := 0
	freshResults := pool.Run(ctx, jobs, opts.workers, func(ctx context.Context, p enumerate.Product) verdict.ProductResult {
		return testProduct(ctx, client, limiter, opts.appType, p)
	}, func(r verdict.ProductResult) {
		tested++
		cacheStore.Upsert(r.Handle, r.Title, r.URL, string(r.Verdict), r.Reason, now)
		switch {
		case bar != nil:
			bar.Add(ui.VerdictKind(r.Verdict))
		case opts.verbose:
			fmt.Printf("    [%d/%d] %s %s\n", tested, total, ui.VerdictLabel(r.Verdict), r.Handle)
		case tested%25 == 0 || tested == total:
			ui.Info("Tested %d/%d...", tested, total)
		}
	})
	if bar != nil {
		bar.Finish()
	}

	freshResults = retryErrored(ctx, client, limiter, opts.appType, byHandle, freshResults, cacheStore, now)

	ui.Step(3, 3, "Saving results")

	allResults, storeFindings := mergeResults(freshResults, enumResult.Products, cacheStore, mode)

	if limiter.GivenUp() {
		storeFindings = append(storeFindings, "The store's Cloudflare bot protection persistently blocked automated access, so some products could not be tested. This is not a fault in the store's Realift setup. Try again later, from a different network/IP, or with a lower --rate.")
	} else if n := limiter.TotalChallenges(); n > 0 {
		storeFindings = append(storeFindings, fmt.Sprintf("The store rate-limited us %d time(s) during the run (Cloudflare); the crawl paced itself down to get through. Re-running in --mode quick will re-check anything left as ERROR.", n))
	}

	if len(newHandles) > 0 {
		storeFindings = append(storeFindings, fmt.Sprintf("%d new products discovered since the last run.", len(newHandles)))
	}
	if len(goneHandles) > 0 {
		storeFindings = append(storeFindings, fmt.Sprintf("%d previously-seen products are no longer listed by the store.", len(goneHandles)))
	}

	cacheStore.EnumMethod = enumResult.Method
	cacheStore.LastRun = now
	if mode == "full" {
		cacheStore.LastFullRun = now
	}
	if err := cacheStore.Save(opts.cacheDir); err != nil {
		ui.Warn("could not save cache: %v", err)
	} else {
		ui.Success("Cache saved")
	}

	reportPath, err := report.Write(opts.outDir, report.Input{
		GeneratedAt:   now,
		StoreURL:      opts.store,
		CanonicalHost: canonical,
		AppType:       opts.appType,
		Mode:          mode,
		EnumMethod:    enumResult.Method,
		TotalProducts: len(enumResult.Products),
		Results:       allResults,
		StoreFindings: storeFindings,
		NewProducts:   len(newHandles),
		GoneProducts:  len(goneHandles),
	})
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	ui.Success("Report written to %s", reportPath)

	ui.PrintSummary(allResults)

	counts := report.Tally(allResults)
	pass, fail := counts[verdict.PASS], report.FailTotal(counts)
	skip, errored := counts[verdict.SkipNotRelevant], counts[verdict.Errored]
	storeName := displayStoreName(enumResult.ShopName, canonical)

	notify.Done(
		"fad-qa: run complete",
		fmt.Sprintf("%s (%s): %d passed, %d failed, %d skipped, %d error", storeName, opts.appType, pass, fail, skip, errored),
		!opts.noSound, !opts.noNotify,
	)

	postSlack(ctx, client, slack.Report{
		StoreName: storeName,
		StoreURL:  canonical,
		AppType:   opts.appType,
		Mode:      mode,
		Date:      now,
		Products:  len(enumResult.Products),
		Pass:      pass, Fail: fail, Skip: skip, Errored: errored,
		Findings: storeFindings,
		Failures: slackFailures(allResults),
	})
	return nil
}

// displayStoreName prefers the homepage-derived shop name, falling back to
// the canonical host.
func displayStoreName(shopName, canonical string) string {
	if s := strings.TrimSpace(shopName); s != "" {
		return s
	}
	return strings.TrimPrefix(strings.TrimPrefix(canonical, "https://"), "http://")
}

// slackFailures extracts the failing products for the Slack summary, capped
// so the message stays within Slack's block limits.
func slackFailures(results []verdict.ProductResult) []slack.Failure {
	var out []slack.Failure
	for _, r := range results {
		if !r.Verdict.IsFail() {
			continue
		}
		out = append(out, slack.Failure{Title: r.Title, URL: r.URL, Verdict: string(r.Verdict), Reason: r.Reason})
		if len(out) >= 50 {
			break
		}
	}
	return out
}

// postSlack posts the run summary to Slack if a .env with a webhook is found
// next to the binary (or in the working directory). Best-effort: a missing
// config is silent, a delivery failure is a warning, never fatal.
func postSlack(ctx context.Context, client *http.Client, r slack.Report) {
	var cfg *slack.Config
	for _, dir := range []string{defaultBaseDir(), "."} {
		c, ok, err := slack.Load(dir)
		if err != nil {
			ui.Warn("could not read .env for Slack in %s: %v", dir, err)
			continue
		}
		if ok {
			cfg = c
			break
		}
	}
	if cfg == nil {
		return // no Slack configured — silently skip
	}
	if err := cfg.Post(ctx, client, r); err != nil {
		ui.Warn("Slack report failed: %v", err)
		return
	}
	ui.Success("Slack report sent%s", channelSuffix(cfg.Channel))
}

func channelSuffix(ch string) string {
	if ch == "" {
		return ""
	}
	return " to " + ch
}

// mergeResults combines this run's freshly-tested results with, in quick
// mode, the still-valid cached verdicts for products that weren't
// retested — so the report reflects the store's complete current state,
// not just the narrow retest subset. It also derives store-level findings
// that don't belong on any single product row (e.g. no include keywords
// configured at all, or the store's keyword config changed since caching).
func mergeResults(fresh []verdict.ProductResult, products []enumerate.Product, cacheStore *cache.StoreCache, mode string) ([]verdict.ProductResult, []string) {
	var findings []string
	tested := make(map[string]bool, len(fresh))
	results := make([]verdict.ProductResult, 0, len(products))

	for _, r := range fresh {
		results = append(results, r)
		tested[r.Handle] = true
	}

	if mode == "quick" {
		for _, p := range products {
			if tested[p.Handle] {
				continue
			}
			st, ok := cacheStore.Products[p.Handle]
			if !ok {
				continue // never tested (brand new); surfaced via "new products" finding instead
			}
			results = append(results, verdict.ProductResult{
				Handle: p.Handle, Title: st.Title, URL: st.URL,
				Verdict: verdict.Verdict(st.LastVerdict), Reason: st.LastReason,
			})
		}
	}

	var observedInclude, observedExclude []string
	var sawDebugContext, includeConfigured bool
	for _, r := range fresh {
		if r.Debug == nil {
			continue
		}
		sawDebugContext = true
		observedInclude = r.Debug.IncludeKeywordList()
		observedExclude = r.Debug.ExcludedKeywords
		if r.Debug.RealiftKeywordsPresent {
			includeConfigured = true
		}
		break
	}
	if sawDebugContext && !includeConfigured {
		findings = append(findings, "No include keywords are configured for this store's app metafield — no product will show the button via keyword matching (product/collection metafields can still resolve it directly).")
	}
	if sawDebugContext && cacheStore.KeywordsChanged(observedInclude, observedExclude) {
		findings = append(findings, "The store's include/exclude keyword configuration changed since the last run.")
	}
	if sawDebugContext {
		cacheStore.RealiftKeywords = observedInclude
		cacheStore.ExcludedKeywords = observedExclude
	}

	return results, findings
}

func testProduct(ctx context.Context, client *http.Client, limiter *fetch.Limiter, appType string, p enumerate.Product) verdict.ProductResult {
	base := verdict.ProductResult{Handle: p.Handle, Title: p.Title, URL: p.URL, ProductType: p.ProductType, AppType: appType}

	result, err := fetch.GetPage(ctx, client, limiter, p.URL)
	if err != nil {
		base.Verdict = verdict.Errored
		base.Reason = fmt.Sprintf("Fetch failed after retries: %v", err)
		return base
	}

	switch {
	case result.StatusCode == http.StatusNotFound:
		base.Verdict = verdict.Gone
		base.Reason = "Product page returned 404 (likely removed since enumeration)."
		return base
	case result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden:
		base.Verdict = verdict.Errored
		base.Reason = fmt.Sprintf("Product page returned HTTP %d (store may have become password-protected mid-run).", result.StatusCode)
		return base
	case result.StatusCode != http.StatusOK:
		base.Verdict = verdict.Errored
		base.Reason = fmt.Sprintf("Unexpected HTTP status %d.", result.StatusCode)
		return base
	}

	// Defensive: a 200 whose body is actually a Cloudflare challenge (rare,
	// depends on CF config) must not be read as "SDK disabled" just because
	// the realift tags are absent.
	if fetch.IsChallengeBody(result.Body) {
		base.Verdict = verdict.Errored
		base.Reason = "Page returned a Cloudflare bot-challenge instead of the product page."
		return base
	}

	extracted := verdict.Extract(result.Body)
	if extracted.Truncated {
		if big, bigErr := fetch.GetPageLarge(ctx, client, limiter, p.URL); bigErr == nil && big.StatusCode == http.StatusOK {
			extracted = verdict.Extract(big.Body)
		}
	}

	in := verdict.Input{Handle: p.Handle, Title: p.Title, URL: p.URL, ProductType: p.ProductType}
	return verdict.Classify(in, extracted, appType)
}

// retryErrored re-tests any product that came back ERROR from the main
// concurrent pass, one at a time at the slowest safe pace. By the time the
// main pass finishes a transiently rate-limited store has usually cooled
// down, so a patient serial retry recovers most of what would otherwise be
// reported as untested. It is skipped entirely when the store has proven it
// is persistently blocking us (retrying would just re-block and waste
// minutes), and it gives the per-IP reputation score a head-start to decay
// if the main run hit any Cloudflare challenges.
func retryErrored(ctx context.Context, client *http.Client, mainLimiter *fetch.Limiter, appType string, byHandle map[string]enumerate.Product, results []verdict.ProductResult, cacheStore *cache.StoreCache, now time.Time) []verdict.ProductResult {
	var toRetry []enumerate.Product
	for _, r := range results {
		if r.Verdict == verdict.Errored {
			if p, ok := byHandle[r.Handle]; ok {
				toRetry = append(toRetry, p)
			}
		}
	}
	if len(toRetry) == 0 {
		return results
	}

	if mainLimiter.GivenUp() {
		ui.Warn("%d products could not be tested and the store is still blocking automated access — skipping retry (it would only re-block).", len(toRetry))
		return results
	}

	ui.Warn("%d products were not reachable — retrying slowly, one at a time", len(toRetry))

	// If the main run tripped Cloudflare, wait for the per-IP score to decay
	// before hammering again, otherwise the retry pass just re-trips it.
	if mainLimiter.TotalChallenges() > 0 {
		const settle = 60 * time.Second
		ui.Info("Letting the store's rate limit settle for %s first...", ui.FormatDuration(settle))
		select {
		case <-time.After(settle):
		case <-ctx.Done():
			return results
		}
	}

	retryLimiter := fetch.NewLimiter(1, 1) // concurrency 1, fresh give-up budget
	recovered := 0
	retried := pool.Run(ctx, toRetry, 1, func(ctx context.Context, p enumerate.Product) verdict.ProductResult {
		return testProduct(ctx, client, retryLimiter, appType, p)
	}, nil)

	updated := make(map[string]verdict.ProductResult, len(retried))
	for _, r := range retried {
		updated[r.Handle] = r
		cacheStore.Upsert(r.Handle, r.Title, r.URL, string(r.Verdict), r.Reason, now)
		if r.Verdict != verdict.Errored {
			recovered++
		}
	}
	for i, r := range results {
		if u, ok := updated[r.Handle]; ok {
			results[i] = u
		}
	}
	ui.Success("Recovered %d/%d previously-unreachable products on retry", recovered, len(toRetry))
	return results
}

func writeAbortReport(ctx context.Context, client *http.Client, opts options, enumResult enumerate.EnumResult, now time.Time) error {
	reason := "Unknown enumeration failure."
	switch {
	case !enumResult.IsShopify:
		reason = "Domain does not appear to be a Shopify storefront."
	case enumResult.PasswordLock:
		reason = "Storefront is password-protected; cannot enumerate or test products."
	case len(enumResult.Products) == 0:
		reason = "No products discoverable via products.json, collections/all, or sitemap.xml."
	}
	findings := append([]string{reason}, enumResult.Warnings...)

	canonical := enumResult.CanonicalHost
	if canonical == "" {
		canonical = opts.store
	}

	path, err := report.Write(opts.outDir, report.Input{
		GeneratedAt:   now,
		StoreURL:      opts.store,
		CanonicalHost: canonical,
		AppType:       opts.appType,
		Mode:          opts.mode,
		EnumMethod:    enumResult.Method,
		TotalProducts: 0,
		StoreFindings: findings,
	})
	if err != nil {
		return fmt.Errorf("write abort report: %w", err)
	}
	ui.Fail("Could not test this store: %s", reason)
	ui.Info("Report written to %s", path)

	storeName := displayStoreName(enumResult.ShopName, canonical)
	notify.Done("fad-qa: run could not complete", fmt.Sprintf("%s (%s): %s", storeName, opts.appType, reason), !opts.noSound, !opts.noNotify)
	postSlack(ctx, client, slack.Report{
		StoreName: storeName,
		StoreURL:  canonical,
		AppType:   opts.appType,
		Mode:      opts.mode,
		Date:      now,
		Findings:  findings,
	})
	return nil
}
