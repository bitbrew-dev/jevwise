# Go SDK

[Documentation index](../README.md#documentation)

## Go SDK quickstart

Import the public SDK from `github.com/bitbrew-dev/jevwise/pkg/typesafe`. The CLI executable is `jevwise`.

Migration: replace previous `github.com/benbenbang/ts-jev-go-sdk/pkg/typesafe` imports with the new path. No old-module compatibility alias is provided; SDK APIs and configuration names are unchanged.

```go
package main

import (
    "context"
    "fmt"

    "github.com/bitbrew-dev/jevwise/pkg/typesafe"
)

func main() {
    client, err := typesafe.NewClient(typesafe.ClientOptions{}) // TYPESAFE_API_KEY
    if err != nil { panic(err) }
    defer client.Close()

    ctx, cancel := context.WithTimeout(context.Background(), typesafe.DefaultTimeout)
    defer cancel()
    result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
        State: "Which task should I tackle first?",
        Questions: map[string]typesafe.Question{
            "decision": typesafe.Choice{Criteria: map[string]typesafe.JSONContent{
                "Fix the bug": nil, "Write documentation": nil,
            }},
        },
    })
    if err != nil { panic(err) }
    fmt.Println(result.Choices()["decision"].Probabilities)
}
```

| Need | API |
| --- | --- |
| Yes/no, choices, rubric scores | `Noul`, `Choice`, `Score` |
| Future/custom question fields | `RawQuestion` |
| Available models | `client.ListModels(ctx)` |
| Raw JSON and HTTP metadata | `SystemOneRaw`, `ListModelsRaw` |
| Independent custom schema | `SystemOneInto(ctx, request, &destination)` |
| Decode previously returned raw bytes | `raw.Decode(&destination)` |
| Per-call settings | Optional `RequestOptions` argument |

- SDK string options resolve explicit values > corresponding `TYPESAFE_*` environment > defaults. CLI-only `TS_JEV_*` variables do not configure SDK clients directly.
- `RequestOptions` supports model, timeout, headers, extra body, and retry policy. Model-list calls accept only timeout, headers, and retry.
- Model precedence: client default < nonempty request model < non-nil per-call model < extra-body model. A model pointer preserves explicit empty text.
- Extra body shallowly replaces fields, including nulls, after original state/questions are validated. Final overrides are not revalidated.
- Default timeout is 10 seconds **per attempt**. Set a caller context deadline to bound the entire SDK operation.
- Nil retry policy inherits defaults/client policy. `Retry: &typesafe.RetryPolicy{}` disables retries. Overrides replace the complete policy.
- Defaults: two retries, HTTP 408/429/500-599, connection/timeouts, 500 ms exponential backoff capped at 5 s, subtractive 25% jitter, 30 s retry budget.
- Retry budget includes attempts and waits but does not cancel in-flight work. Caller context expiration always stops further retries. Server retry delays are respected without the backoff cap.
- A predicate adds retryable errors to built-in rules. HTTP and typed/custom decoding share an attempt; standalone `RawResponse.Decode` never retries.
- Injected HTTP clients remain caller-owned. `Close` releases only SDK-owned idle connections. Configuration maps are copied; do not mutate inputs while constructing/preparing a call.
- Use `errors.As` with `APIError`, `ConnectionError`, `TimeoutError`, or `ResponseValidationError`. Inspectable raw bodies, headers, and causes may contain sensitive data; do not log them indiscriminately.
