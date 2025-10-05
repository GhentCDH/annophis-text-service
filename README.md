[![Go Report Card](https://goreportcard.com/badge/github.com/GhentCDH/annophis-text-service)](https://goreportcard.com/report/github.com/GhentCDH/annophis-text-service)

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
* **No ellipses are inserted** into text; if content is clipped/truncated, responses include `complete: false`.
* **CORS** via the `ORIGIN_ALLOWED` environment variable.

---

## Quick start

### Requirements

* Go **1.25+**

### Build & run (local)

```bash
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

Create `config.json`:

```json
{
  "host": "0.0.0.0",
  "port": ":8080",
  "cex_source": "https://cdn.jsdelivr.net/gh/ThomasK81/CTSTextservice@master/cex/",
  "test_cex_source": "https://cdn.jsdelivr.net/gh/ThomasK81/CTSTextservice@master/cex/million.cex"
}
```

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

> The service never inserts ellipses. If content is clipped or truncated, `complete` is `false`.

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

# Anchored range across nodes
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.0@forth[1]-1.1

# Anchored range within one node (start and end anchors)
curl http://127.0.0.1:8080/million/texts/urn:cts:greekLit:tlg0016.tlg001.eng:1.0@forth[1]-@Herodotus[1]
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
