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
middleware CPU and allocation cost against a benign `GET /api/resource` request
with no matching WAF rules.

---

## Benchmark results

Machine: `darwin/amd64`, VirtualApple @ 2.50 GHz (Apple M-series, Rosetta),
Go 1.26.

```
goos: darwin
goarch: amd64
pkg: github.com/vibewarden/vibewarden/test/benchmarks
cpu: VirtualApple @ 2.50GHz
BenchmarkProxy_DirectPassthrough-10      2159046    1672 ns/op    5394 B/op    14 allocs/op
BenchmarkProxy_WithSecurityHeaders-10    1521925    2350 ns/op    6226 B/op    21 allocs/op
BenchmarkProxy_WithRateLimiting-10       1916719    1851 ns/op    5402 B/op    15 allocs/op
BenchmarkProxy_WithWAF-10               1000000    3572 ns/op   13638 B/op    16 allocs/op
BenchmarkProxy_AllMiddleware-10          808497    4416 ns/op   14478 B/op    24 allocs/op
```

### Interpretation

| Benchmark | ns/op | Overhead vs baseline | B/op | allocs/op |
|---|---|---|---|---|
| DirectPassthrough | 1 672 | — (baseline) | 5 394 | 14 |
| WithSecurityHeaders | 2 350 | +678 ns (+0.7 µs) | 6 226 | 21 |
| WithRateLimiting | 1 851 | +179 ns (+0.2 µs) | 5 402 | 15 |
| WithWAF | 3 572 | +1 900 ns (+1.9 µs) | 13 638 | 16 |
| AllMiddleware | 4 416 | +2 744 ns (+2.7 µs) | 14 478 | 24 |

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

- **WAF** is the most expensive single middleware at ~1.9 µs overhead per
  request. This cost is dominated by the regular-expression scan over query
  parameters, selected headers, and the first 8 KB of the request body.
  Requests with long bodies or many query parameters will see higher values;
  static assets and API calls with small payloads sit near the benchmark figure.

- **AllMiddleware** stacks all three layers and adds ~2.7 µs total. The
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
- **Benign requests only**: benchmarks send clean GET requests. WAF rule
  matching on malicious inputs (many regex matches before a block) is more
  expensive than the benign-request path shown here.

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
