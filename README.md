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

## Build

```
go build -o fad-qa .
```

Cross-compile for another platform:

```
GOOS=windows GOARCH=amd64 go build -o fad-qa-windows-amd64.exe .
GOOS=darwin  GOARCH=arm64 go build -o fad-qa-darwin-arm64 .
```

## Usage

```
./fad-qa --store https://example.myshopify.com --app realfoot
```

```
Usage of ./fad-qa:
  -app string
        app type: realfoot | realhand | realbody | foot3d (required)
  -cache string
        directory holding per-store cache files (default "./cache")
  -mode string
        test mode: full | quick (quick retests only previously-failing products) (default "full")
  -no-keepawake
        don't prevent the machine from sleeping during the run
  -no-notify
        disable desktop notification
  -no-sound
        disable completion sound
  -out string
        directory to write the Markdown report to (default "./reports")
  -rate float
        steady-state requests per second (default 6)
  -store string
        Shopify store URL to test (required)
  -verbose
        print per-product progress
  -workers int
        max concurrent requests (1-32) (default 8)
```

- `--mode full` (default) tests every discovered product.
- `--mode quick` retests only products that failed (or errored) last run,
  using the per-store cache under `--cache`. It still re-enumerates the
  store's product list (cheap) so it can warn about new or removed
  products, but only re-fetches product pages for the retest set.

Re-running against the same store reuses the cache automatically — no
flags needed beyond `--mode quick`.

## Output

A Markdown report is written to `--out` (default `./reports`), named
`<store>__<app>__<date>.md`, containing: run metadata, a pass/fail/skip/
error summary, store-level findings (e.g. no include keywords configured),
and a table of failing products with title, URL, reason, and a suggested
keyword fix where one can be inferred.

## Project layout

See `CLAUDE.md` for the package map, verdict matrix, defaults, and
workflow rules (commit granularity, branching model).
