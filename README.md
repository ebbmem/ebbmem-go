# ebbmem-go

Go client for the Ebb memory HTTP API. Version 0.2.0. Homepage: https://ebbmem.ai.

This directory is the module `github.com/ebbmem/ebbmem-go`. It does not import the rest of the Ebb repository. `scripts/sync-go-sdk.sh` copies it into a checkout of that module.

This module is not yet published. When it is, install it from the package registry:

```bash
go get github.com/ebbmem/ebbmem-go@v0.2.0
```

## Quickstart

```go
client, err := ebbmem.NewClient() // reads EBBMEM_ORIGIN and EBBMEM_TOKEN
stored, err := client.Memory.Remember(ctx, "the harbor light is cobalt", nil, nil, nil, "")
found, err := client.Memory.Recall(ctx, "harbor light", nil, nil)
```

Every call takes `context.Context`.

## Auth

`EBBMEM_ORIGIN` is required. There is no default origin. `EBBMEM_TOKEN` is sent as `Authorization: Bearer`. `EBBMEM_API_KEY` is sent as `X-API-Key`. `EBBMEM_NAMESPACE` is sent as `X-Ebb-Namespace`. An explicit option overrides the matching environment variable. `String()` does not include the token.

## Methods

`client.Memory` exposes `Remember`, `RememberBatch`, `Recall`, `RecallFederated`, `Get`, `List`, `Update`, `Forget`, `Reinforce`, `Reflect`, `WhyNot`, and `Status`. `client.Tasks` exposes `Get`, `List`, and `Cancel`. `client.Events.Stream` calls the SSE operation. `client.Health` calls the health endpoint.

## Errors and retries

Failures return `*ebbmem.Error` (`Status`, `Code`, `Message`, `Details`, `RequestID`). Wrappers are `*AuthError` (401/403), `*NotFoundError` (404), `*ValidationError` (400/422), `*RateLimitError` (429, `RetryAfter`), `*ServerError` (5xx), and `*ConnectionError`. Use `errors.As`. Each wrapper unwraps to `*ebbmem.Error`.

The client retries connection errors, 429 (honoring `Retry-After`), and 502/503/504. Retries apply to idempotent methods, and to `Remember` / `RememberBatch` when an idempotency key is set. `WithMaxRetries` defaults to 2. The timeout defaults to 30 seconds. The user agent is `ebbmem-client-go/0.2.0`.

## Pagination

`Memory.List` returns `iter.Seq2` and follows `next_cursor` until `has_more` is false.

## Events

`Events.Stream(ctx, tokenQuery)` calls the generated events operation. Pass the query token only when the stream must authenticate that way.

## Raw API

`client.Raw()` returns the generated API client.

## License

Proprietary, (c) Spectrayan.
