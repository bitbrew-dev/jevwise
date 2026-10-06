# Python compatibility and Go adaptations

Baseline: [TypeSafe Python SDK v0.7.2](https://github.com/typesafe-ai/typesafe-sdk-python/tree/f078f1e208a0d885154dc758344ae4fce77ac168), revision `f078f1e208a0d885154dc758344ae4fce77ac168`.

## Request and response contract

- Endpoints remain `POST /v1/systemone` and `GET /v1/models`. Typed requests support Noul, Choice, Score, structured string/object/array content, nested nulls, and raw future fields.
- Go additionally validates original state shape before extra-body merging; Python normalizes original questions but defers state validation to the server. Overrides shallowly replace fields and are not revalidated.
- Go's request model adds a tier between client default and per-call model. Empty request model inherits; a non-nil call model preserves blank/whitespace strings. Extra-body model has final priority.
- Typed answers preserve integer score keys and nullable/absent token counts. Unknown future answer types are omitted from typed maps but retained in raw bytes; Go emits no Python-style warning.
- `SystemOneInto` accepts an independent Go destination, rather than a Pydantic response model. Required-field/custom validation belongs to that destination. Failed JSON decoding can partially modify it.
- Successful SDK response decoding attaches `Raw`; independent destinations obtain metadata from the returned raw response. Standalone raw decoding makes no requests or retries.
- Response validation errors expose safe constant strings with inspectable paths/causes, not Python's complete validation-error HTTP metadata. Go response structs/maps remain mutable rather than frozen.

## HTTP ownership and safety

- One context-aware client replaces Python's separate synchronous/asynchronous clients. Injected HTTP clients remain caller-owned, including idle connections. `Close` only releases owned idle connections.
- Go's timeout is a per-attempt context deadline, not HTTPX's separate phase timeouts. Zero inherits the SDK timeout, independently of an injected HTTP client's timeout; the shorter effective deadline wins.
- Redirects are blocked and response bodies are limited to 8 MiB. Authentication and SDK/retry headers are protected from caller overrides.
- Raw bodies, headers, error causes, and request IDs are inspectable but may be sensitive. Error strings sanitize endpoints and known API keys; CLI service failure strings also suppress arbitrary server/input text.

## Retry semantics

- Defaults match the pinned SDK: 2 retries, HTTP 408/429/500-599, connection/timeouts, 500 ms exponential backoff capped at 5 s, subtractive 25% jitter, and a 30 s budget.
- Nil policies inherit defaults/client settings; explicit zero policy disables retries. Zero budget means unbounded in Go, corresponding to Python's `None`. Per-call policies replace rather than merge.
- Built-in matches short-circuit the additive predicate. Caller cancellation/deadlines take precedence regardless of predicate results.
- HTTP and optional decoding run inside each attempt. Prepared request bytes and headers are replayed; retry-count header is absent initially, then `1`, `2`, and so on.
- Attempts and delays consume budget, but budget exhaustion does not interrupt in-flight work. No next attempt begins when elapsed time plus its delay reaches/exceeds budget; oversleep is rechecked.
- `retry-after-ms` has priority. Blank/whitespace means zero; invalid/negative/nonfinite milliseconds fall through to `Retry-After` seconds or date. Hexadecimal numeric strings are rejected to match Python. Valid server delays are not capped by the backoff maximum.
- Go uses nanosecond precision rather than Python's millisecond-rounded jitter, standard HTTP date formats rather than permissive email dates, and rejects delays outside `time.Duration` range.

Source files: `_core/client/sync/client.py`, `_core/config.py`, `_core/endpoints.py`, `_core/questions.py`, `_core/response_types.py`, `_core/schemas/base.py`, `_core/transport.py`, `_core/retry.py`, `_core/errors.py`, and `tests/test_retry.py` at the pinned revision.
