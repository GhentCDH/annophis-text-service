[![CI](https://github.com/GhentCDH/annophis-text-service/actions/workflows/ci.yml/badge.svg)](https://github.com/GhentCDH/annophis-text-service/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/GhentCDH/annophis-text-service.svg)](https://pkg.go.dev/github.com/GhentCDH/annophis-text-service)

# annophis-text-service

A tiny HTTP service for exposing **CTS/CEX** texts with a modern API, written in Go (Chi router).
This project is a **modernisation** of the CITE Architecture’s microservices: [cite-architecture/citemicroservices](https://github.com/cite-architecture/citemicroservices).

## Features

* Reads a CEX file (remote or local) and serves:

    * `/texts` (list of work stems)
    * `/texts/{URN}` (single, prefix, range, anchored, and regex-anchored lookups)
    * `/texts/urns/{URN}` (expand a URN or range to concrete URNs)
    * `/texts/first|last|previous|next/{URN}`
    * `/texts/catalog` (parsed from `#!ctscatalog`)
    * `/texts/version`, `/cite`, `/healthz`
* **Anchored URNs:** `urn:...:<ref>@needle[n]` (or `@/regex/`) and **ranges with anchors**.
* **Unicode normalization** of responses via `?normalize=` (`nfc` default, `nfd`, `nfkc`, `nfkd`, `strip`) — see [Text encoding & normalization](#text-encoding--normalization).
* **No ellipses are inserted** into text; if content is clipped/truncated, responses include `complete: false`.
* **CORS** via the `ORIGIN_ALLOWED` environment variable.

---

## Quick start

### Requirements

* Go **1.25+**

### Build & run (local)

```bash
# Make sure dependencies are met
make tidy
# build binary to ./bin
make build

# run from source (uses ./config.json)
make run
```

The service listens on `:8080` by default.

### Docker

```bash
# build image
make docker-build

# run container with your config mounted
make docker-run PORT=8080
```

The image exposes port `8080` and includes a `/healthz` healthcheck.

---

## Configuration

Copy the tracked example and adjust it (`config.json` itself is gitignored):

```bash
cp config.example.json config.json
```

```json
{
  "host": "0.0.0.0",
  "port": ":8080",
  "cex_source": "https://cdn.jsdelivr.net/gh/ThomasK81/CTSTextservice@master/cex/",
  "test_cex_source": "https://cdn.jsdelivr.net/gh/ThomasK81/CTSTextservice@master/cex/million.cex"
}
```

The Docker image bakes in `config.example.json` as its default `/app/config.json`; override it with a bind mount or the `CONFIG` env var.

* If `cex_source` **ends with `.cex`**, it is treated as a single file.
* If it is a **directory base**, you can select a file by:

    * Path prefix: `/{CEX}/texts/...` (example: `/million/texts`)
    * Or query: `?cex=million`

Environment variables:

* `CONFIG` — path to the config file (default `/app/config.json` in Docker).
* `ORIGIN_ALLOWED` — comma-separated list of allowed origins for CORS (example: `http://localhost:5173`).

---

## API

### Version and health

* `GET /cite` — service family and version.
* `GET /texts/version` — texts API version.
* `GET /healthz` — health probe (checks CEX source reachability).

### Catalog

* `GET /texts/catalog`
* `GET /{CEX}/texts/catalog`

Returns parsed entries from `#!ctscatalog`.

### Work list

* `GET /texts`
  Returns work **stems** (first 4 URN parts plus a trailing colon), for example:

  ```json
  { "urn": ["urn:cts:greekLit:tlg0016:", "urn:cts:latinLit:phi1038:"] }
  ```

### URN expansion

* `GET /texts/urns/{URN}`

    * Exact URN returns itself.
    * Prefix returns all matching URNs.
    * Range `a-b` returns URNs from the first `a*` through the last `b*` (inclusive).

### Navigation

* `GET /texts/first/{URN}`
* `GET /texts/last/{URN}`
* `GET /texts/previous/{URN}`
* `GET /texts/next/{URN}`

`{URN}` must be a valid CTS URN.

### Passages

* `GET /texts/{URN}`
* `GET /{CEX}/texts/{URN}`

`{URN}` forms:

* **Exact:** `urn:cts:greekLit:tlg0016.tlg001.eng:1.1`
* **Prefix:** `urn:cts:greekLit:tlg0016.tlg001.eng:1`
* **Range:** `...:1.1-1.2`
* **Anchored:** `...:1.1@Persians[1]` or `...:1.1@/Per(s|z)ians/[1]`
  (regex delimiters `/` must be percent-encoded as `%2F` in the URL path)
* **Anchored range:**

    * Across nodes: `...:1.0@forth[1]-1.1`
      Start in `1.0` at the first `forth`, then return `1.0` from that match to its end, then return full `1.1`.
    * Within one node: `...:1.0@forth[1]-@Herodotus[1]`
      Start at the first `forth`, then stop at the first `Herodotus` inside `1.0`.

#### Optional query parameters

* `substring` — with `clip=true`, returns a window around the first match (case-insensitive).
* `clip` (bool) — when `true`, return a snippet; when `false`, return the full passage.
  For **anchored** URNs the default is `clip=true`.
* `context` (int) — number of runes around the match (default `0` for anchored URNs).
* `maxChars` (int) — hard cap on text length (no ellipsis; sets `complete=false` when truncated).
* `tail` (bool) — with anchored URNs, return from the match then to the end of the passage.
* `ignoreAccents` (bool) — with anchored URNs, match the anchor needle **diacritic-insensitively** (e.g. `Περσαι` matches `Πέρσαι`). Matching stays case-insensitive; offsets are reported against the original, accented text. Applies to plain-string anchors (single and range), not to `@/regex/` anchors.
* `normalize` — Unicode normalization form applied to the **returned text** (see [Text encoding & normalization](#text-encoding--normalization)). One of `nfc` (default), `nfd`, `nfkc`, `nfkd`, `strip`. An unsupported value returns `400`.

> The service never inserts ellipses. If content is clipped or truncated, `complete` is `false`.

---

## Text encoding & normalization

Working with Greek (and Latin, Arabic, and other scripts that lean on combining
characters, precomposed forms, and ligatures) means the *same-looking* text can
be encoded in different ways. This service takes an explicit position on that.

### Why NFC is the stored/default form

On load, every passage is normalised to **NFC** (Normalization Form C,
*composed*) and served as NFC by default. NFC:

* is the **interchange standard** (recommended by the W3C) and what most clients,
  editors, and fonts expect;
* gives a **single, deterministic encoding** for canonically-equivalent text, so
  it is a stable baseline for the service's **anchor matching** (an anchor needle
  is NFC-normalised before it is searched, so it lines up with the stored text).

All matching, clipping, and offset logic runs on this NFC baseline. The
`normalize` parameter is a **response-time transform only** — it never mutates
the stored CEX data and never changes how anchors are resolved.

### `normalize` options

| Value    | Meaning | Typical use |
|----------|---------|-------------|
| `nfc`    | *(default)* Composed: precomposed code points where they exist (`ά` = U+03AC). | Display, interchange. |
| `nfd`    | Decomposed: base letter + combining marks (`ά` = `α` + `◌́`). Uniformly `base + marks`. | Collation, alignment, per-mark processing. |
| `nfkc`   | Composed **compatibility** form: also folds compatibility variants (ligature `ﬁ`→`fi`, micro sign `µ`→Greek `μ`, ohm `Ω`→`Ω`). Lossy. | Normalising typographic variants while keeping accents. |
| `nfkd`   | Decomposed compatibility form. Lossy. | As above, decomposed. |
| `strip`  | `nfkd` + **drop combining marks** + **expand letter ligatures** (`œ`→`oe`, `æ`→`ae`, `ß`→`ss`) → a diacritic-free base-character sequence. | Full-text search, HTR/NLP pre-processing, matching `Herodotus` against `Hēródotus`. |

> **NFC vs NFD, briefly.** Both are lossless and reversible — the same
> characters, encoded differently. NFC composes to single code points where it
> can; NFD always splits into `base + marks`. NFD is not "more unique" than NFC —
> both are canonical — but its uniform decomposition is what makes accent
> stripping and collation easy, which is why `strip` is built on the K-forms.

### Limitations: looks the same, genuinely different

Normalization only unifies characters that Unicode considers **equivalent**. It
will **not** merge look-alikes that are actually different characters — and no
`normalize` value (not even `strip`) fixes these:

* **Cross-script homoglyphs.** Greek `Α` (U+0391), Latin `A` (U+0041), and
  Cyrillic `А` (U+0410) render identically but are three distinct letters.
  Likewise Greek `Ο/ο/Ρ` vs Latin `O/o/P`. Detecting these needs a *confusables*
  table (Unicode [UTS #39](https://www.unicode.org/reports/tr39/)), which is out
  of scope here.
* **Compatibility variants** (micro sign vs mu, ohm sign vs omega, ligatures) are
  only folded by the **K-forms** (`nfkc`/`nfkd`/`strip`), and that folding is
  lossy — do not use it when the exact code point matters.
* **No precomposed form.** Some polytonic Greek combinations (e.g. macron +
  accent) have no single NFC code point, so even NFC text can legitimately
  contain combining marks. Use `nfd`/`strip` if you need a uniform base sequence.

---

## Response shapes

### Node

```json
{
  "urn": ["urn:cts:...:1.1"],
  "text": ["..."],               // full or clipped
  "previous": ["urn:cts:...:1.0"],
  "next": ["urn:cts:...:1.2"],
  "sequence": 1768,              // 1-based index in file order
  "complete": true               // false when clipped
}
```

### NodeResponse

```json
{
  "requestUrn": ["<the URN you asked for>"],
  "status": "Success",
  "service": "/texts",
  "nodes": [ /* Node[] */ ]
}
```

---

## Examples

Assuming `cex_source` is a directory base and `million.cex` is available:

```bash
# List work stems
curl http://127.0.0.1:8080/million/texts

# Catalog
curl http://127.0.0.1:8080/million/texts/catalog

# Exact node
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.1

# Range
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.1-1.2

# Anchored substring (first occurrence of "Persians")
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.1@Persians[1]

# Accent-insensitive anchor (unaccented needle matches accented Greek)
curl "http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Περσαι[1]?ignoreAccents=true"

# Anchored range across nodes
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.0@forth[1]-1.1

# Anchored range within one node (start and end anchors)
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.0@forth[1]-@Herodotus[1]

# Decomposed (NFD) output
curl "http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1?normalize=nfd"

# Diacritic-free, ligature-expanded base text (for search/collation)
curl "http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1?normalize=strip"
```

---

## Development

### Project layout

```
.
├─ cmd/annophis-text-service/   # main
├─ internal/server/             # router, handlers, helpers
│  ├─ server.go                 # Server, config, router, healthz
│  ├─ handlers_basic.go         # /cite, /texts/version, /texts, /texts/catalog
│  ├─ handlers_texts.go         # /texts/{URN}, nav, urns, anchored/range logic
│  ├─ helpers.go                # helpers (JSON writer, indexing, etc.)
├─ config.json                  # example config
├─ Dockerfile
└─ Makefile
```

### Common tasks

```bash
make tidy   # go mod tidy
make test   # (when tests exist)
```

---

## License

See `LICENSE` in the repository.

## Credits

Development by [Ghent Centre for Digital Humanities - Ghent University](https://www.ghentcdh.ugent.be/) and by [YouSayData](https://www.yousaydata.com/). Funded by the [GhentCDH research projects](https://www.ghentcdh.ugent.be/projects).

<img src="https://www.ghentcdh.ugent.be/ghentcdh_logo_blue_text_transparent_bg_landscape.svg" alt="Landscape" width="500">
