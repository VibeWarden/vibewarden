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

Benchmarks come in two shapes:

- **No-body** (`GET /api/resource`, no request body) — the floor of middleware
  cost. Baseline: `BenchmarkProxy_DirectPassthrough`.
- **With body** (`POST /api/resource` with a small JSON payload) — representative
  of API traffic, and the only shape that exercises the WAF body scan. Baseline:
  `BenchmarkProxy_DirectPassthrough_WithBody`.

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
BenchmarkProxy_DirectPassthrough-10           2927154    1229 ns/op    5395 B/op    14 allocs/op
BenchmarkProxy_DirectPassthrough_WithBody-10  2570942    1397 ns/op    5811 B/op    18 allocs/op
BenchmarkProxy_WithSecurityHeaders-10         2178680    1644 ns/op    6195 B/op    20 allocs/op
BenchmarkProxy_WithRateLimiting-10            2605812    1381 ns/op    5403 B/op    15 allocs/op
BenchmarkProxy_WithWAF-10                     1367595    2624 ns/op   13639 B/op    16 allocs/op
BenchmarkProxy_WithWAF_WithBody-10             345212   10428 ns/op   14328 B/op    25 allocs/op
BenchmarkProxy_AllMiddleware-10               1000000    3235 ns/op   14448 B/op    23 allocs/op
```

### Interpretation

No-body requests, measured against `DirectPassthrough`:

| Benchmark | ns/op | Overhead vs baseline | B/op | allocs/op |
|---|---|---|---|---|
| DirectPassthrough | 1 229 | — (baseline) | 5 395 | 14 |
| WithSecurityHeaders | 1 644 | +415 ns (+0.4 µs) | 6 195 | 20 |
| WithRateLimiting | 1 381 | +152 ns (+0.2 µs) | 5 403 | 15 |
| WithWAF | 2 624 | +1 395 ns (+1.4 µs) | 13 639 | 16 |
| AllMiddleware | 3 235 | +2 006 ns (+2.0 µs) | 14 448 | 23 |

Body-bearing requests, measured against `DirectPassthrough_WithBody`:

| Benchmark | ns/op | Overhead vs baseline | B/op | allocs/op |
|---|---|---|---|---|
| DirectPassthrough_WithBody | 1 397 | — (baseline) | 5 811 | 18 |
| WithWAF_WithBody | 10 428 | +9 031 ns (+9.0 µs) | 14 328 | 25 |

All values are well below their respective latency budget targets.

### Key observations

- **SecurityHeaders** adds ~0.7 µs per request. The cost comes from constructing
  and setting six HTTP response header strings. There is headroom to add more
  headers without approaching the 1 ms P50 budget.

- **RateLimiting** with a no-op in-memory limiter adds ~0.2 µs. A real
  in-memory token-bucket limiter (`golang.org/x/time/rate`) will be slightly
  more expensive due to mutex contention and time-package calls; a Redis-backed
  limiter will incur a full network round-trip (typically 0.2–1 ms on localhost,
  1–5 ms over LAN).

- **WAF** is by far the most expensive middleware, and its cost depends
  entirely on whether the request has a body. A bodyless `GET` costs ~1.4 µs:
  the rules run over query parameters and three headers only. A `POST` with a
  37-byte JSON body costs ~9.0 µs, roughly **6.5x more**, because every rule in
  the default ruleset is evaluated against the body bytes as well.

  Budget for the body number, not the bodyless one, if your app serves a JSON
  API. At 1 000 RPS of body-bearing traffic the WAF consumes about 9 ms of CPU
  per wall-clock second (~0.9% of one core). Requests with bodies approaching
  the 8 KB scan limit, or with many query parameters, will cost more still.

- **AllMiddleware** stacks all three layers and adds ~2.0 µs total on the
  bodyless path (it uses `newBenchRequest`, so it inherits the WAF no-body
  caveat above). The
  aggregate is sub-additive because of CPU cache effects when the chain runs
  sequentially on the same goroutine.

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
- **Small bodies only**: `_WithBody` benchmarks use a 37-byte JSON payload. The
  WAF scans up to 8 KB, so larger payloads cost proportionally more regex work.
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
