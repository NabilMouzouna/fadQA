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

## Getting started

### Prerequisites

- Go 1.25 or newer (only needed to build; the output is a single binary
  with no runtime dependencies).
- The URL of a Shopify store to test, and which Realift app it runs:
  `realfoot`, `realhand`, `realbody`, or `foot3d`.

### 1. Build the binary

```
go build -o fad-qa .
```

To build for a different machine (e.g. testing from a Mac but running on a
teammate's Windows laptop):

```
GOOS=windows GOARCH=amd64 go build -o fad-qa-windows-amd64.exe .
GOOS=darwin  GOARCH=arm64 go build -o fad-qa-darwin-arm64 .
```

### 2. Run a full test

```
./fad-qa --store https://example.myshopify.com --app realfoot
```

This enumerates every product the store publishes, tests each product
page, and prints a live progress bar with an upfront time estimate. First
runs on a large catalog can take a while — the estimate at the top of
"Test Run" tells you roughly how long before you start.

### 3. Read the report

Every run writes a Markdown file to `./reports/<store>__<app>__<date>.md`
(also printed at the end of the run), containing:

- Run metadata (date, store, app type, product count)
- A pass/fail/skip/error summary
- Store-level findings (e.g. no include keywords configured)
- A table of failing products with title, URL, the reason, and a
  suggested keyword fix where one can be inferred

### 4. Fix issues, then re-test only what failed

Once the team has acted on the report, re-run in quick mode instead of
re-testing the whole catalog:

```
./fad-qa --store https://example.myshopify.com --app realfoot --mode quick
```

Quick mode reuses the cache under `./cache/` from the previous run,
re-checks the store's product list (cheap — a handful of requests even for
a large catalog) to catch new or removed products, but only re-fetches
product pages for items that previously failed or errored. Nothing extra
to configure — the cache is keyed automatically by store and app type.

## Flags reference

| Flag | Default | Meaning |
|---|---|---|
| `--store` | *(required)* | Shopify store URL to test |
| `--app` | *(required)* | `realfoot` \| `realhand` \| `realbody` \| `foot3d` |
| `--mode` | `full` | `full` tests every product; `quick` retests only previous failures/errors |
| `--workers` | `8` | Max concurrent requests (1–32) |
| `--rate` | `6` | Steady-state requests per second |
| `--out` | `./reports` | Directory to write the Markdown report to |
| `--cache` | `./cache` | Directory holding per-store cache files |
| `--verbose` | off | Print a line per product instead of a progress bar |
| `--no-sound` | off | Disable the completion sound |
| `--no-notify` | off | Disable the desktop notification |
| `--no-keepawake` | off | Allow the machine to sleep during the run |

If a store rate-limits aggressively (common on `*.shopifypreview.com`
dev/preview domains), you don't need to tune these — the tool backs off
automatically and retries anything that still comes back unreachable at
the end of the run, one at a time, before giving up on it.

## Project layout

See `CLAUDE.md` for the package map, verdict matrix, defaults, and
workflow rules (commit granularity, branching model).
