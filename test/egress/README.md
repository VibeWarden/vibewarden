# Egress Proxy Integration Tests

Tests the VibeWarden egress proxy using a local httpbin container. No internet access required.

## Run

```bash
cd test/egress
docker compose up -d
./test.sh
docker compose down
```

## What it tests

| Test | What it verifies |
|------|-----------------|
| Transparent mode | `X-Egress-URL` header routes to httpbin |
| Named route | `/_egress/httpbin-get/get` resolves correctly |
| POST forwarding | JSON body forwarded intact |
| Default deny | Unlisted URLs get 403 Forbidden |
| Method enforcement | DELETE on GET-only route gets 403 |
| Header injection | `X-Injected-By` added by egress config |
| Header stripping | `Cookie` and `X-Secret-Internal` removed before forwarding |

## Architecture

```
app (Alpine) → vibewarden:8081 (egress proxy) → httpbin (local container)
                  ↑                                    ↑
            route matching,                    echoes request as JSON
            header manipulation,               (no internet needed)
            default deny
```
