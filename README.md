# fad-qa

Automated QA for the Realift size-measurement button on Shopify stores — no
AI, no headless browser. It crawls a store's product pages over plain HTTP,
reads the signals Shopify's theme already renders into the page, and
reports whether the button shows up and, if not, exactly why.

## How it works

Every product page renders two JSON script tags and a custom element
server-side, unconditionally, whether or not anyone opens the in-browser
debug console:

- `#realift-config` — resolves whether a size chart applies to this
  product (`sizeChart` empty ⟺ button hidden).
- `#realift-debug-context` — why: keyword match, product/collection
  metafield, excluded, or nothing matched at all.
- `<realift-button>` — whether the button block is even present on this
  product's template (themes with multiple product templates sometimes
  miss it on some of them).

Reading these three signals from the raw HTML is enough to reproduce
everything the `?realift-debug-console=show` panel shows, without
executing any JavaScript. See `CLAUDE.md` for the full technical writeup.

## Getting started (for teammates — no Go, no repo checkout)

### macOS

```
curl -fsSL https://raw.githubusercontent.com/NabilMouzouna/fadQA/main/install.sh | bash
```

Downloads the latest release for your Mac (Intel or Apple Silicon,
detected automatically) and drops `./fad-qa` in the current directory.
Then run it — see "Usage" below.

### Windows

```
curl.exe -L -o fad-qa.zip https://github.com/NabilMouzouna/fadQA/releases/latest/download/fad-qa-windows-amd64.zip
tar -xf fad-qa.zip
```

(Use `fad-qa-windows-arm64.zip` instead if you're on Windows on ARM.)
This extracts a `fad-qa-windows-amd64` folder containing `fad-qa.exe`. The
first time you run it, SmartScreen will likely say "Windows protected
your PC" — click **More info** → **Run anyway**. This only happens once
per machine. Then run it — see "Usage" below.

### Manual download (either platform)

Grab the zip for your platform from the [latest
release](https://github.com/NabilMouzouna/fadQA/releases/latest) and
unzip it yourself instead of using the commands above:

- **macOS**: the binary isn't signed, so the first launch needs one extra
  step — right-click `fad-qa` → **Open** → confirm "Open" in the dialog.
  (Or run `xattr -d com.apple.quarantine ./fad-qa` once in Terminal.)
- **Windows**: same SmartScreen click-through as above.

`cache/` and `reports/` folders are created automatically right next to
the binary the first time they're needed, wherever that binary happens to
live. Nothing to clone, nothing to install.

## Usage

### 1. Run a full test

```
./fad-qa --store https://example.myshopify.com --app realfoot
```

This enumerates every product the store publishes, tests each product
page, and prints a live progress bar with a running pass/fail/skip/error
tally underneath. First runs on a large catalog can take a while,
especially if the store rate-limits — see the note on that below.

### 2. Read the report

Every run writes a Markdown file to `reports/<store>__<app>__<date>.md`
next to the binary (path also printed at the end of the run), containing:

- Run metadata (date, store, app type, product count)
- A pass/fail/skip/error summary
- Store-level findings (e.g. no include keywords configured)
- A table of failing products with title, URL, the reason, and a
  suggested keyword fix where one can be inferred

### 3. Fix issues, then re-test only what failed

Once the team has acted on the report, re-run in quick mode instead of
re-testing the whole catalog:

```
./fad-qa --store https://example.myshopify.com --app realfoot --mode quick
```

Quick mode reuses the cache under `cache/` from the previous run,
re-checks the store's product list (cheap — a handful of requests even for
a large catalog) to catch new or removed products, but only re-fetches
product pages for items that previously failed or errored. Nothing extra
to configure — the cache is keyed automatically by store and app type.

## Slack reporting (optional)

When a run finishes, the tool always writes the Markdown report and shows a
desktop notification + sound. It can **also** post a summary to Slack.

To enable it, drop a `.env` file **next to the binary** (same folder) with
your Slack Incoming Webhook:

```
SLACK-WEBHOOK-TOKEN=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<secret>
SLACK-CHANNEL=#realift-qa
```

- `SLACK-WEBHOOK-TOKEN` — your Incoming Webhook. The full `https://...` URL
  or just the trailing path token both work.
- `SLACK-CHANNEL` — informational only (an Incoming Webhook always posts to
  the channel it was created for).

If `.env` is absent (or has no webhook), Slack posting is silently skipped —
so the same binary works with or without it. The `.env` is **never committed
to git** (it's gitignored) and must be distributed to teammates separately
from the public binary. Every completed run posts a summary: status counts,
store name + URL, app type, date, mode, product count, and the top failing
products.

## Building and distributing (for whoever maintains this tool)

### Prerequisites

- Go 1.25 or newer (only needed to build; the output is a single binary
  with no runtime dependencies).

### Build for your own machine

```
go build -o fad-qa .
```

### Build and package for the whole team

```
./build.sh
```

This cross-compiles for macOS (Intel + Apple Silicon) and Windows (amd64 +
ARM64), and zips each one with the README into `dist/` (gitignored) —
ready to hand out as-is. Point each teammate at the zip for their platform
and the "Getting started" section above.

To build a single platform manually instead:

```
GOOS=windows GOARCH=amd64 go build -o fad-qa-windows-amd64.exe .
GOOS=darwin  GOARCH=arm64 go build -o fad-qa-darwin-arm64 .
```

### Publishing a new release

The curl install commands above always pull the **latest** GitHub
Release, so cutting a new one is what ships an update to the team:

```
./build.sh
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

Then create a release for that tag on GitHub and upload the four zips
from `dist/` as its assets (via the GitHub web UI, or `gh release create
vX.Y.Z dist/*.zip` if you have the GitHub CLI installed).

## Flags reference

| Flag | Default | Meaning |
|---|---|---|
| `--store` | *(required)* | Shopify store URL to test |
| `--app` | *(required)* | `realfoot` \| `realhand` \| `realbody` \| `foot3d` |
| `--mode` | `full` | `full` tests every product; `quick` retests only previous failures/errors |
| `--workers` | `4` | Max concurrent requests, 1–32 (a ceiling — see below) |
| `--rate` | `4` | Max steady-state requests per second (a ceiling) |
| `--out` | `reports/` next to the binary | Directory to write the Markdown report to |
| `--cache` | `cache/` next to the binary | Directory holding per-store cache files |
| `--verbose` | off | Print a line per product instead of a progress bar |
| `--no-sound` | off | Disable the completion sound |
| `--no-notify` | off | Disable the desktop notification |
| `--no-keepawake` | off | Allow the machine to sleep during the run |

## About speed and rate limiting

Shopify storefronts sit behind **Cloudflare bot protection**, which limits
how fast *any* automated client can crawl from a single IP — this is not
something the tool (or your Realift setup) is doing wrong. Cloudflare tracks
a per-IP reputation score; push too hard and it starts serving "verify your
connection" challenges to everything for a few minutes.

So `--workers` and `--rate` are **ceilings, not fixed speeds**. The tool
starts slow, ramps up while the store stays happy, and the moment it sees a
Cloudflare challenge it drops to a crawl and **pauses to let the limit
clear** (you'll see a `[waiting]` message with a countdown — that's normal,
not a freeze). It resumes automatically. If a store blocks automated access
persistently, the tool gives up cleanly and says so in the report rather
than grinding forever.

Practical notes:
- Large catalogs from one IP take a while — minutes, not seconds — and that
  is the polite, safe speed. Raising `--rate`/`--workers` past the defaults
  usually just trips Cloudflare sooner and ends up **slower** overall.
- Re-run with `--mode quick` after a fix: it only re-fetches products that
  previously failed/errored, so it's far lighter on the rate limit.
- `*.shopifypreview.com` preview/dev-store URLs are throttled much harder
  than a live production storefront — expect more pausing on those.

## Project layout

See `CLAUDE.md` for the package map, verdict matrix, defaults, and
workflow rules (commit granularity, branching model).
