# VibeWarden — Performance benchmarks and latency budget

This document explains how much latency VibeWarden adds to requests,
what the latency budget is for each middleware layer, and how to reproduce
the measurements yourself.

---

## Latency budget

| Layer | Target P50 overhead | Target P99 overhead |
|---|---|---|
| Direct passthrough (no middleware) | baseline | baseline |
| Security headers only | < 1 ms | < 2 ms |
| Rate limiting only | < 1 ms | < 2 ms |
| WAF only | < 2 ms | < 5 ms |
| All middleware combined | < 3 ms | < 10 ms |

The sidecar target is **< 1 ms P50** and **< 5 ms P99** overhead for a simple
proxy (no WAF). With all middleware enabled the target is **< 5 ms P50** and
**< 10 ms P99**. The benchmark numbers below are single-machine in-process
measurements and represent the middleware cost in isolation, not end-to-end
network round-trip time.

---

## Running the benchmarks

```bash
# Run all benchmarks with memory allocation stats (5-second run per bench)
go test -bench=. -benchmem -benchtime=5s ./test/benchmarks/

# Run a single benchmark
go test -bench=BenchmarkProxy_WithWAF -benchmem ./test/benchmarks/

# Increase iterations for more stable numbers
go test -bench=. -benchmem -count=3 -benchtime=5s ./test/benchmarks/
```

Benchmarks live in `test/benchmarks/proxy_bench_test.go`. They use
`net/http/httptest` so no network stack is involved. The numbers measure pure
middleware CPU and allocation cost against a benign request with no matching
WAF rules.

Benchmarks come in two shapes. Both carry two query parameters
(`?page=2&sort=created_at`) and a 117-byte browser `User-Agent`, because those
are what the WAF actually inspects on a bodyless request:

- **No-body** (`GET /api/resource?page=2&sort=created_at`) — a browser-style GET.
  Baseline: `BenchmarkProxy_DirectPassthrough`.
- **With body** (the same request as a `POST` with a 37-byte JSON payload) — the
  only shape that exercises the WAF body scan.
  Baseline: `BenchmarkProxy_DirectPassthrough_WithBody`.

Always compare a `_WithBody` benchmark against the `_WithBody` baseline. A POST
with a body costs more to construct than a bodyless GET, and that construction
cost must not be attributed to the middleware.

---

## Benchmark results

Machine: `darwin/arm64`, Apple M1 Max, Go 1.27. Median of `-count=3`,
`-benchtime=3s`.

```
goos: darwin
goarch: arm64
pkg: github.com/vibewarden/vibewarden/test/benchmarks
cpu: Apple M1 Max
BenchmarkProxy_DirectPassthrough-10           2584827    1394 ns/op    5827 B/op    16 allocs/op
BenchmarkProxy_DirectPassthrough_WithBody-10  2489506    1448 ns/op    5891 B/op    19 allocs/op
BenchmarkProxy_WithSecurityHeaders-10         1994570    1798 ns/op    6627 B/op    22 allocs/op
BenchmarkProxy_WithRateLimiting-10            2298481    1560 ns/op    5835 B/op    17 allocs/op
BenchmarkProxy_WithWAF-10                      128472   27929 ns/op   14523 B/op    21 allocs/op
BenchmarkProxy_WithWAF_WithBody-10             101457   35348 ns/op   14730 B/op    29 allocs/op
BenchmarkProxy_AllMiddleware-10                125074   28715 ns/op   15346 B/op    28 allocs/op
```

### Interpretation

No-body requests, measured against `DirectPassthrough`:

| Benchmark | ns/op | Overhead vs baseline | B/op | allocs/op |
|---|---|---|---|---|
| DirectPassthrough | 1 394 | — (baseline) | 5 827 | 16 |
| WithSecurityHeaders | 1 798 | +404 ns (+0.4 µs) | 6 627 | 22 |
| WithRateLimiting | 1 560 | +166 ns (+0.2 µs) | 5 835 | 17 |
| WithWAF | 27 929 | +26 535 ns (+26.5 µs) | 14 523 | 21 |
| AllMiddleware | 28 715 | +27 321 ns (+27.3 µs) | 15 346 | 28 |

Body-bearing requests, measured against `DirectPassthrough_WithBody`:

| Benchmark | ns/op | Overhead vs baseline | B/op | allocs/op |
|---|---|---|---|---|
| DirectPassthrough_WithBody | 1 448 | — (baseline) | 5 891 | 19 |
| WithWAF_WithBody | 35 348 | +33 900 ns (+33.9 µs) | 14 730 | 29 |

All values are well below their respective latency budget targets. The WAF is
the only layer whose cost varies by more than a rounding error with the request,
and it is the one to size against your own traffic — see below.

### Key observations

- **SecurityHeaders** adds ~0.4 µs per request. The cost comes from constructing
  and setting six HTTP response header strings. There is headroom to add more
  headers without approaching the 1 ms P50 budget.

- **RateLimiting** with a no-op in-memory limiter adds ~0.2 µs. A real
  in-memory token-bucket limiter (`golang.org/x/time/rate`) will be slightly
  more expensive due to mutex contention and time-package calls; a Redis-backed
  limiter will incur a full network round-trip (typically 0.2–1 ms on localhost,
  1–5 ms over LAN).

- **WAF** is by far the most expensive middleware, roughly two orders of
  magnitude above the other two layers. Its cost is driven by **how many bytes
  it inspects**, not by which part of the request they came from. `ScanRequest`
  evaluates the entire default ruleset once per query-parameter value, once for
  each of the three inspected headers that is present (`Cookie`, `Referer`,
  `User-Agent`), and once over the first 8 KB of the body.

  The two benchmarks make the byte cost readable. They differ only by the
  37-byte body, and that body costs +7.4 µs, so on this hardware a full ruleset
  sweep runs at roughly **0.2 µs per inspected byte**. The bodyless GET inspects
  128 bytes (two short query values plus a 117-byte browser `User-Agent`) for
  +26.5 µs, which is consistent with the same rate.

  **Size the WAF against the bytes your requests carry, not against the presence
  or absence of a body.** The body is not automatically the dominant term: at
  that rate the 117-byte `User-Agent` accounts for most of the +26.5 µs bodyless
  number, several times what the 37-byte payload costs. A bodyless GET with a
  long `User-Agent` or a handful of query parameters can easily cost more than a
  small `POST`. At 1 000 RPS of the body-bearing shape above the WAF consumes
  about 34 ms of CPU per wall-clock second (~3.4% of one core).

  Extrapolating the per-byte rate, a request that fills the 8 KB body-scan limit
  lands in the low milliseconds — the same order as the < 2 ms P50 WAF budget.
  That extrapolation is not benchmarked here; measure it against your own payload
  sizes if you accept large request bodies.

- **AllMiddleware** stacks all three layers for +27.3 µs on the bodyless shape:
  the WAF number plus the ~0.6 µs the other two layers cost in isolation. It uses
  `newBenchRequest`, so add roughly the body-scan increment above for a JSON
  `POST`.

---

## Benchmark scope and limitations

- **No network I/O**: benchmarks use `httptest.NewRecorder()` and
  `httptest.NewRequest()`. Real request latency includes TCP/TLS overhead.
- **No auth middleware**: authentication (Ory Kratos session validation) is
  excluded because it requires an HTTP round-trip to Kratos. Expect 1–10 ms
  additional overhead depending on session-cache hit rate.
- **No-op rate limiter**: the in-process limiter used here has no mutex
  contention. A Redis-backed limiter adds a network round-trip per request.
- **No-op metrics collector**: the Prometheus registry write path is omitted
  so the WAF and rate-limit numbers isolate the middleware logic only.
- **Benign requests only**: benchmarks send clean requests. WAF rule matching on
  malicious inputs (many regex matches before a block) is more expensive than
  the benign-request path shown here.
- **One request shape only**: the benchmarks use two short query parameters, one
  inspected header, and (for `_WithBody`) a 37-byte JSON payload. WAF cost scales
  with the total inspected bytes, so a request with more query parameters, a
  `Cookie`, or a payload approaching the 8 KB scan limit costs proportionally
  more regex work.
- **Upstream does not drain the body**: the benchmark upstream handler never
  reads the request body, so the cost of reading back the body the WAF restored
  is not included. It is a few hundred nanoseconds for small payloads.

---

## Regression tracking

To detect performance regressions, run the benchmarks with `-count=5` and
compare results using the `benchstat` tool:

```bash
# Capture a baseline on the main branch
go test -bench=. -benchmem -count=5 ./test/benchmarks/ > old.txt

# After your change
go test -bench=. -benchmem -count=5 ./test/benchmarks/ > new.txt

# Compare
benchstat old.txt new.txt
```

Any increase in ns/op greater than 10% for a given benchmark should be
investigated before merging to main.
