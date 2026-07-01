# fad-qa — context for future sessions

## What this is

A standalone Go CLI that QA-tests whether the Realift size-measurement
button shows up correctly on a Shopify client store's product pages, and if
not, says exactly why. Zero AI dependency, zero headless browser — it's a
plain HTTP crawler that reads signals Shopify's Liquid theme already renders
into every product page's raw HTML.

This is a companion tool to the main app at
`/Users/nabilmouzouna/Dev/realife/scan-apps/mono-alpha/mono` (the "realift"/
"realSize" SaaS). It does not share code or a build with that repo — it's a
separate Go module that reverse-engineers the *externally observable*
behavior of that app's Shopify theme extension.

## The ground-truth finding that makes this possible

Read directly from `apps/shopify/extensions/realift-button/blocks/
realift-sdk.liquid` and `realift-button.liquid` in the main repo (as of the
session that built this tool — verify against current source if the SDK
changes). Two facts matter:

1. **The show/hide decision is computed server-side in Liquid and baked
   into every product page's HTML unconditionally** — not gated behind
   `?realift-debug-console=show`. A plain `GET /products/{handle}` reveals
   everything the in-browser debug console shows.
2. Two JSON script tags plus one custom element carry all the signal:
   - `<script type="application/json" id="realift-config">` →
     `{account, measurementId, app, sizeChart, style}`. `sizeChart` empty
     (null/""/{}/[]) ⟺ button hidden; non-empty ⟺ visible. This is the
     single ground-truth signal for visibility (client JS shows the button
     iff `Shopify.designMode` — false on live storefronts — OR
     `sizeChart` is present).
   - `<script type="application/json" id="realift-debug-context">` →
     `resolution_source` (`none|keyword_match|product_metafield|
     collection_metafield|excluded`), `is_excluded`, `matched_keyword/
     field/value`, `excluded_keyword`, `excluded_keywords_present` (whether
     the store customized its exclude list vs. using the app's hardcoded
     fallback), and the store's raw `realift_keywords` (an object mapping
     keyword → `"app:sizeChart:style"`, not an array — see
     `verdict.DebugContext.IncludeKeywordList()`).
   - `<realift-button>` custom element — rendered by a **separate** app
     block placed into the product template's section. Its presence is
     independent of the config tag (a template can have the SDK embed
     enabled store-wide but be missing the button block on a specific
     product template — the most common "button doesn't exist" complaint,
     since some themes have multiple product templates).

The app's hardcoded fallback exclusion list (used when the store hasn't
customized `app.metafields.sizechart.excludedkeywords.value`) is exactly:
`wallet, belt, sock, insole, outsole, card, bag, backpack, purse, accessor`.

If the SDK's Liquid output shape ever changes, re-verify against
`apps/shopify/extensions/realift-button/blocks/*.liquid` in the main repo
before trusting this tool's verdicts.

## Verdict matrix (`internal/verdict/verdict.go`)

| Observed in HTML | Verdict | Debug console equivalent |
|---|---|---|
| No `#realift-config` | `FAIL_SDK_OFF` | — |
| Config present, no `<realift-button>` | `FAIL_NO_BUTTON_BLOCK` | `0 (0 visible)` |
| Button, `sizeChart` empty, source `none` | `FAIL_NOT_INCLUDED` (or `SKIP_NOT_RELEVANT` if the relevance dictionary is confident the product is out of scope) | `1 (0 visible)` |
| Button, `sizeChart` empty, source `excluded` | `FAIL_EXCLUDED` (or `SKIP_NOT_RELEVANT` if hidden by the hardcoded fallback list for realfoot/foot3d) | `1 (0 visible)` |
| Button, `sizeChart` non-empty | `PASS` (plus `Advisory: WARN_UNEXPECTED_VISIBLE` if the relevance dictionary thinks it's out of scope) | `1 (1 visible)` |
| Network/5xx/timeout exhausted retries | `ERROR` — never counted as a FAIL | — |
| 404 | `GONE` — product removed since enumeration | — |

**Advisory relevance is never authoritative.** The per-app-type keyword
dictionaries in `internal/verdict/relevance.go` can only soften an
unmatched/excluded hide into `SKIP_NOT_RELEVANT`, or flag an unexpectedly
visible `PASS` for review — they can never override the ground-truth
`sizeChart` signal or manufacture a `FAIL` on their own.

## Package map

```
internal/enumerate/  Shopify detection (redirects, password-lock), product
                      discovery via /products.json → /collections/all →
                      sitemap.xml fallback chain, dedupe by handle.
internal/fetch/       Shared http.Client, AIMD adaptive rate/concurrency
                      limiter (halves on 429/503, grows after a clean
                      streak), exponential backoff + Retry-After handling.
internal/verdict/     Streaming HTML extraction (x/net/html tokenizer, no
                      full DOM) of the three signals, the verdict
                      classifier, and the advisory relevance dictionaries.
internal/pool/        Generic bounded worker pool (goroutines + channel).
internal/cache/       Per-store-and-app-type JSON cache (atomic write) for
                      full-vs-quick reruns.
internal/report/      Markdown report renderer.
internal/notify/      Cross-platform sound/desktop notification (beeep) and
                      best-effort keep-awake (build-tag gated per OS).
main.go               CLI flags + orchestration.
```

## Defaults worth knowing

- Concurrency: 8 workers default (1–32 range), rate 6 req/s steady-state,
  AIMD halves both on 429/503 and grows by 1 after 20 clean successes.
- Retry: 5 attempts, exponential backoff 500ms×2^n capped at 30s + full
  jitter; `Retry-After` overrides when present (capped at 120s).
- Body cap 4MB per page, one-shot retry at 16MB if a script tag looks
  truncated.
- `/products.json` pages at 250/page (Shopify's max), hard stop at page
  400 as a runaway safety net.
- Cache files: `cache/<host>__<apptype>.json`, schema-versioned, corrupt
  files backed up to `.bad` rather than silently discarded.
- Quick mode always re-enumerates (cheap — a handful of JSON requests even
  for thousands of products) so new/removed products are always detected;
  it only skips the expensive per-product-page fetch for products that
  already passed last run.

## Build / test

```
go build -o fad-qa .
go test ./...          # full suite, all packages, incl. -race clean
go vet ./...
gofmt -l .              # should be empty

# cross-compile (no cgo, verified clean for all four):
GOOS=darwin  GOARCH=arm64 go build -o fad-qa-darwin-arm64 .
GOOS=darwin  GOARCH=amd64 go build -o fad-qa-darwin-amd64 .
GOOS=windows GOARCH=amd64 go build -o fad-qa-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o fad-qa-windows-arm64.exe .
```

## Workflow rules for this repo

- **Commit granularity**: each commit is one small, completed unit of work
  (one package, one fix, one doc update) — never one huge commit bundling
  unrelated changes. If a change touches multiple independent pieces,
  split it into multiple commits.
- **Commit messages**: descriptive, plain prose, no AI attribution or
  generated-by boilerplate (no "Co-Authored-By", no "Generated with
  Claude Code" or similar trailers).
- **Branches**: two long-lived branches — `main` (stable) and `dev`
  (active development). Day-to-day work happens on `dev`; merge to `main`
  deliberately when a set of changes is ready.

## Session log

- **2026-07-01**: Initial build. Explored the main repo to establish the
  ground-truth detection facts above (three parallel Explore agents +
  direct source reads of the two `.liquid` files). Designed and implemented
  the full tool: enumerate/fetch/verdict/pool/cache/report/notify packages,
  `main.go` orchestration, unit tests for every package (verdict matrix +
  traps, extraction fixtures incl. attribute-order independence, enumerate
  pagination/sitemap/detection via `httptest`, limiter AIMD behavior, cache
  round-trip/atomicity, pool concurrency/cancellation, report rendering).
  All tests pass including `-race`. Cross-compiled cleanly for
  darwin/windows × arm64/amd64 with zero cgo.
  **Not yet done**: a live smoke test against a real client store with the
  Realift SDK installed, to cross-check verdicts against the in-browser
  `?realift-debug-console=show` panel — deliberately skipped this session
  (no test store was provided). Do this before relying on the tool for a
  real client engagement.
  Wrote README.md and this file, set up the git repo with `main`/`dev`
  branches and pushed to `github.com/NabilMouzouna/fadQA`, split across
  small per-package commits per the workflow rules above.
