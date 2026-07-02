# fad-qa — context for future sessions

> **This repo (`github.com/NabilMouzouna/fadQA`) is public as of
> 2026-07-02**, to support unauthenticated `curl`-based install for
> teammates. Everything in this file, including the reverse-engineered
> Realift internals below, is publicly visible — keep that in mind before
> adding anything more sensitive here.

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
internal/fetch/       Browser-like http.Client (Chrome UA, headers, cookie
                      jar); adaptive ramp-up limiter that self-discovers a
                      safe speed and handles Cloudflare challenges distinctly
                      from Shopify quota 429s (challenge.go); backoff +
                      Retry-After. See the Cloudflare section below.
internal/verdict/     Streaming HTML extraction (x/net/html tokenizer, no
                      full DOM) of the three signals, the verdict
                      classifier, and the advisory relevance dictionaries.
internal/pool/        Generic bounded worker pool (goroutines + channel).
internal/cache/       Per-store-and-app-type JSON cache (atomic write) for
                      full-vs-quick reruns.
internal/report/      Markdown report renderer.
internal/notify/      Cross-platform sound/desktop notification (beeep) and
                      best-effort keep-awake (build-tag gated per OS). The
                      completion notification body now carries pass/fail/
                      skip/error counts.
internal/slack/       Optional Slack Incoming Webhook reporting. Reads .env
                      (dotenv.go; keys SLACK-WEBHOOK-TOKEN / SLACK-CHANNEL)
                      from next to the binary (main.go falls back to cwd),
                      builds a Block Kit summary (payload.go, JSON-marshalled
                      so user text is escaped), POSTs on every completed run.
                      Absent .env → silently skipped; delivery failure →
                      warning, never fatal.
internal/ui/          Terminal presentation: color/TTY detection, phased
                      section/step headers, a two-line live progress bar
                      (schollz/progressbar on top, a spaced-out pass/fail/
                      skip/error tally below it, redrawn via raw ANSI
                      cursor movement), and the final summary panel. Shows
                      elapsed time, not a predicted ETA — rate limiting can
                      make throughput collapse mid-run, so a naive linear
                      ETA would be actively misleading rather than just
                      imprecise. Colors and the bar auto-disable when
                      stdout isn't a TTY or NO_COLOR is set — falls back to
                      periodic plain "Tested N/M..." lines.
main.go               CLI flags + orchestration.
```

## The Cloudflare bot-management problem (most important operational fact)

Shopify storefronts sit behind **Cloudflare bot management**, which is what
actually limits us — NOT Shopify's API quota. Empirically confirmed against
a live `*.shopifypreview.com` store (2026-07-02):

- A throttled request returns **HTTP 429 with a `cf-mitigated: challenge`
  header** and a "Verifying your connection..." HTML body — a JavaScript
  challenge, **not** a JSON quota error. There is **no `Retry-After`**.
- It's a **per-IP reputation score**, cumulative over a time window. Serial
  1 req/s is fine; concurrency 8 trips it; after ~150–200 total requests in
  a few minutes the IP flips to "challenge everything" mode where *every*
  request (even 1/20s) gets 429.
- Recovery is **slow: ~4 minutes of near-idle** after heavy flagging.
- A plain HTTP client can NEVER solve the JS challenge — retrying harder is
  futile; only slowing down and letting the score decay works.
- The User-Agent is a *minor* factor (serial requests passed with either
  UA); **concurrency/rate is the real trigger.**

The old design mistook these for Shopify quota-429s and applied escalating
90s cooldowns shared across 8 workers → once flagged, the whole fleet froze
permanently (a real run did **24 products in an hour**). That's the bug the
current design fixes.

## Defaults worth knowing

- Concurrency/rate are **ceilings the adaptive limiter ramps UP toward**,
  not starting values. Default max 4 workers / 4 req/s (was 8/6 — 8 tripped
  Cloudflare in testing). `NewLimiter` starts at 2 workers / 2 req/s and
  adds 1 of each per `rampStreak` (30) clean successes. This self-discovers
  a safe speed per store/IP instead of hardcoding a magic number.
- **Challenge handling** (`internal/fetch/{challenge,limiter,get}.go`):
  `isCloudflareChallenge` distinguishes a CF challenge (cf-mitigated header
  / challenge body markers / no Retry-After) from a genuine Shopify quota
  429 (has Retry-After). On a challenge, `Limiter.OnChallenge` drops to
  concurrency 1 + min rate and opens ONE shared, escalating global cooldown
  for the episode (60s → 120s → 240s); simultaneous detections by other
  workers coalesce into that one episode. After `maxChallengeStreak` (3)
  consecutive escalating episodes with no success between, it **gives up**
  (`ErrBlocked`, latched) so the whole run ends cleanly instead of grinding
  — any single success resets the streak to 0. A genuine Retry-After 429
  still uses the old softer `OnThrottle` + `Cooldown(retryAfter)` path.
- **Browser-like signature** (`client.go`): realistic Chrome UA + Accept /
  Accept-Language / sec-ch-ua / Sec-Fetch headers + a `cookiejar` so the
  session cookies from the first request are echoed back (looks like one
  visitor, not thousands of cookieless hits). NOT evasion — these are
  public pages any browser can fetch; we just avoid looking gratuitously
  robotic. (Do NOT set Accept-Encoding manually — Go only auto-gunzips when
  it added that header itself.)
- **Cooldown visibility**: `Limiter.SetCooldownHook` fires once per episode;
  `main.go` surfaces it via `ProductBar.Note` (clean redraw around the live
  bar) or `ui.Warn`, so a legitimate multi-minute pause never looks frozen.
- Retry budgets (`get.go`): network errors and 5xx get 5 attempts, exp
  backoff 500ms×2^n capped 30s. Retry-After 429 respected (cap 120s).
- **Second-chance pass**: after the main run, `main.go`'s `retryErrored`
  re-tests anything still `ERROR` serially (concurrency 1) with a fresh
  limiter — but it is **skipped if the store gave up** (`limiter.GivenUp()`
  — retrying would just re-block), and it waits 60s first if the main run
  hit any challenges (let the score decay).
- Body cap 4MB per page, one-shot retry at 16MB if a script tag looks
  truncated.
- `/products.json` pages at 250/page (Shopify's max), hard stop at page
  400 as a runaway safety net.
- `enumerate.normalizeBase` only defaults to `https://` when no scheme is
  given — an explicit `http://` is respected, not force-upgraded (useful
  for local testing against a mock store; real Shopify stores are always
  https anyway).
- Cache files: `cache/<host>__<apptype>.json`, schema-versioned, corrupt
  files backed up to `.bad` rather than silently discarded.
- Quick mode always re-enumerates (cheap — a handful of JSON requests even
  for thousands of products) so new/removed products are always detected;
  it only skips the expensive per-product-page fetch for products that
  already passed last run.
- `--out`/`--cache` default to `reports/`/`cache/` next to the *executable*
  (`main.go`'s `defaultBaseDir`, via `os.Executable()` + symlink
  resolution), not the current working directory. This matters once the
  binary is distributed standalone — a double-clicked exe or a shortcut
  can have an unpredictable CWD, but should still self-create its folders
  in a consistent, discoverable place next to itself.

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

# build + package all four for distribution (zips each with README into dist/):
./build.sh
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

- **2026-07-01 (same day, follow-up)**: Two rounds of changes based on
  user feedback after the initial build.
  1. Added `internal/ui` and restructured `main.go`'s terminal output into
     phased sections (Configuration → Step 1/2/3) with a live progress
     bar, an upfront ETA estimate (later removed — see below), and a
     colored final summary panel — requested because the plain scrolling
     log gave no sense of progress or how long a run would take. Exported
     `report.Tally`/`FailTotal` so the terminal summary and the Markdown
     report share one counting
     implementation.
  2. The user ran the tool against a real Shopify **preview/dev-store**
     domain (`*.shopifypreview.com`) and got 81/104 products back as
     `ERROR` (HTTP 429, retries exhausted) — preview domains rate-limit
     much harder than production storefronts, and the original 5-attempt/
     30s-cap retry budget plus purely-local AIMD backoff wasn't enough to
     recover. Fixed with three changes (all in `internal/fetch`): a
     shared `Limiter.Cooldown()` hard-pause triggered on every 429/503 so
     *all* workers back off together (not just soft concurrency halving),
     a separate, more patient retry budget for 429/503 specifically (10
     attempts / 90s cap vs. 5/30s for other errors), and a `main.go`
     `retryErrored` pass that serially re-tests anything still `ERROR`
     after the main run at 1 req/s. Verified against a local mock Shopify
     server simulating both a permanently-429ing product (correctly ends
     as one `ERROR`, no hang) and a product recovering after 11 requests
     (correctly recovered via the second-chance pass) — see
     `internal/fetch/get_test.go` for the equivalent automated coverage.
     Also relaxed `enumerate.normalizeBase` to stop force-upgrading an
     explicit `http://` to `https://` (needed for local mock-store
     testing; discovered while setting up the above verification).
  Restructured README.md's usage section into numbered "Getting started"
  steps plus a flags reference table, per user request for clearer
  first-time-usage docs.

- **2026-07-01 (same day, second follow-up)**: Ran the tool again against
  the same `*.shopifypreview.com` store from the fix above. The run
  legitimately took much longer than the bar's own live ETA predicted
  (stuck at 23% for ~1 minute against a "[2s:9s]" estimate) — the store
  was rate-limiting again and the retry/cooldown machinery was correctly
  being patient, but schollz/progressbar's linear ETA extrapolation has no
  visibility into that and just extrapolates pre-throttle throughput,
  producing a confidently wrong number. Combined with feedback that the
  upfront static estimate (`ui.EstimateDuration`) was similarly untrustworthy,
  removed both: deleted `internal/ui/eta.go` entirely, dropped the "Test
  Run" section's "Estimated time" line in `main.go`, and switched
  `ProductBar` to `OptionSetElapsedTime(true)` / `OptionSetPredictTime(false)`
  — elapsed time is always true, a predicted remaining time under this
  workload usually isn't. Also restructured `ProductBar` into two live
  lines per user request (bar on top, wider-spaced pass/fail/skip/error
  tally below, instead of cramming the tally into the bar's own
  description prefix) using raw ANSI cursor-up/clear-line escapes to
  redraw both in place — see `internal/ui/progress.go`. Not unit-tested
  (terminal escape-sequence output isn't meaningfully testable); verify
  visually in a real terminal if this rendering is touched again.

- **2026-07-02**: User wants teammates to run fad-qa without cloning the
  repo or installing Go — hand them a built binary and have it "just
  work". Two changes:
  1. `--out`/`--cache` previously defaulted to `./reports`/`./cache`
     relative to the process's **current working directory**. That's fine
     for a `cd`-then-run terminal workflow, but breaks for a distributed
     binary launched via double-click or a shortcut, where CWD is
     unpredictable. Added `defaultBaseDir()` in `main.go` (`os.Executable()`
     + `filepath.EvalSymlinks`) so both now default to `reports/`/`cache/`
     next to the binary itself, regardless of invocation CWD. Verified by
     building to a scratch dir and running from an unrelated CWD (`/tmp`)
     — folders correctly appeared next to the binary, not in `/tmp`.
     Folder creation itself was already handled (`os.MkdirAll` in
     `cache.Save`/`report.Write`); the only gap was *where*.
  2. Added `build.sh`: cross-compiles macOS (arm64 + amd64) and Windows
     (amd64 + arm64), zips each with `README.md` into `dist/` (gitignored)
     — a teammate just unzips and runs, no Go or repo needed. Verified a
     packaged zip unzips with the exec bit intact and runs standalone.
     Documented both the teammate quick-start (including macOS Gatekeeper
     right-click-Open and Windows SmartScreen "Run anyway" first-launch
     steps — real friction points for handing out an unsigned binary) and
     the maintainer build/distribute workflow in README.md, splitting it
     from the general "Usage" section since those audiences differ.

- **2026-07-02 (same day, follow-up)**: User wants a one-command `curl`
  install for teammates. This repo was **private**, and a plain
  unauthenticated `curl` against GitHub Release assets only works on a
  public repo — flagged the trade-off (this repo's `CLAUDE.md` documents
  Realift's internal keyword-matching/exclusion logic, reverse-engineered
  from the main app's private source) via AskUserQuestion; user initially
  chose a separate binaries-only public repo, then instead made this repo
  itself public directly. Proceeded on that basis. Set up:
  - Tagged `v0.1.0` on `main`, created a GitHub Release via the REST API
    (no `gh` CLI available in this environment) using the same credential
    already used for `git push` all session, and uploaded the four
    `build.sh` zips as release assets. `https://github.com/<repo>/releases/
    latest/download/<asset>` is a stable redirect that always points at
    whichever release is newest — install commands never need to know a
    version number.
  - `install.sh` (macOS only — checks `uname -s == Darwin`, exits with a
    pointer to the Windows instructions otherwise): `curl -fsSL .../
    install.sh | bash` downloads the right arch's release zip, extracts
    just the `fad-qa` binary into the current directory, chmods it
    executable, and defensively clears `com.apple.quarantine` (curl
    downloads don't set it the way browser downloads do — verified
    empirically: only the newer, non-blocking `com.apple.provenance`
    attribute was present, not `quarantine`, so no Gatekeeper block on
    a terminal-launched run).
  - **Windows explicitly does NOT get `install.sh`** — a `.sh` script
    can't run there without WSL/Git Bash/Cygwin, none of which should be
    assumed present. Windows instead gets a native two-command flow using
    `curl.exe` + `tar` (both ship built into Windows 10 1803+/11 without
    any extra install), documented separately in the README.
  - Verified the *exact* documented command end-to-end from a clean
    directory (`curl -fsSL .../install.sh | bash`) — downloaded, installed,
    and ran the real binary successfully, confirming the whole pipeline
    (public repo → release → asset → install script) actually works, not
    just that each piece looks right in isolation.
  - Learned (and saved to persistent memory): don't proactively pull and
    test stored credentials (e.g. `git credential fill` + a probing API
    call) before deciding how to proceed — just attempt the real intended
    action directly. The user declined exactly that kind of speculative
    check earlier in this same task.
  - Publishing a new release (documented in README): `./build.sh`, tag,
    push tag, then create a GitHub Release for that tag with the four
    `dist/*.zip` files attached as assets.

- **2026-07-02 (Cloudflare rate-limit rework)**: A 2750-product run did only
  24 products in an hour. Investigated empirically against the live store
  (see "The Cloudflare bot-management problem" section above) and confirmed
  the 429s are Cloudflare JS challenges (`cf-mitigated: challenge`, no
  Retry-After), triggered by concurrency/cumulative per-IP volume, with ~4min
  recovery — and that the old shared-90s-cooldown-per-429 logic death-spiralled
  the whole fleet. Reworked `internal/fetch`: added `challenge.go`
  (isCloudflareChallenge / IsChallengeBody), rebuilt `limiter.go` into an
  adaptive ramp-up limiter with a distinct `OnChallenge` path (drop to floor +
  single escalating shared cooldown per episode + give-up after 3 consecutive
  episodes → `ErrBlocked`), and browser-ified `client.go`/`get.go` (Chrome UA,
  browser headers, cookie jar). Lowered defaults to 4/4 ceilings (ramp from
  2/2). `main.go`: cooldown hook → `ProductBar.Note`/`ui.Warn` so pauses are
  visible; `IsChallengeBody` guard so a 200-challenge isn't misread as
  FAIL_SDK_OFF; `retryErrored` now skips when the store gave up and settles
  60s first if the main run was challenged; store-level findings explain a
  blocked/rate-limited run. Live-validated challenge detection, cooldown UI,
  and recovery against the real store (the IP was still flagged from probing,
  so it correctly paused 60s during enumeration then recovered). Unit tests
  cover the limiter escalation/give-up/coalescing and challenge detection.

- **2026-07-02 (Slack reporting + notification enrichment)**: Added
  `internal/slack` — posts a Block Kit summary to a Slack Incoming Webhook on
  every completed run (including abort/blocked runs). Config comes from a
  `.env` next to the binary (keys `SLACK-WEBHOOK-TOKEN`, `SLACK-CHANNEL`,
  matching the user's file; `webhookURL` accepts a full URL or bare token);
  absent `.env` → silently skipped, delivery failure → warning not fatal.
  `.env` is gitignored (verified never committed to history); `.env.example`
  documents the format. Added best-effort store-name extraction in
  `enumerate.detect` (og:site_name → `<title>` first segment → host
  heuristic; `shopname.go`) surfaced as `EnumResult.ShopName` and used in the
  Slack/desktop summaries. Enriched `notify.Done` body with pass/fail/skip/
  error counts. Unit-tested dotenv parsing, webhook URL normalization,
  payload build/escaping/failure-capping, POST success+non-2xx, and shop-name
  extraction. NOTE: not yet verified against the real webhook (would post a
  live message to the team channel) — the pipeline is proven against an
  httptest server; offer a one-off test post before relying on it.
