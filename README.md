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

If someone already built and shared `fad-qa` with you as a zip, this is
all you need:

1. Unzip it and open a terminal in that folder.
2. **macOS**: the binary isn't signed, so the first launch needs one extra
   step — right-click `fad-qa` → **Open** → confirm "Open" in the dialog.
   (Or run `xattr -d com.apple.quarantine ./fad-qa` once in Terminal.)
   After that first approval, it runs normally from the terminal.
3. **Windows**: SmartScreen will likely say "Windows protected your PC" —
   click **More info** → **Run anyway**. This only happens once per
   machine.
4. Run it — see "Usage" below.

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

## Flags reference

| Flag | Default | Meaning |
|---|---|---|
| `--store` | *(required)* | Shopify store URL to test |
| `--app` | *(required)* | `realfoot` \| `realhand` \| `realbody` \| `foot3d` |
| `--mode` | `full` | `full` tests every product; `quick` retests only previous failures/errors |
| `--workers` | `8` | Max concurrent requests (1–32) |
| `--rate` | `6` | Steady-state requests per second |
| `--out` | `reports/` next to the binary | Directory to write the Markdown report to |
| `--cache` | `cache/` next to the binary | Directory holding per-store cache files |
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
