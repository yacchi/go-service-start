# go-service-start

[![Go Reference](https://pkg.go.dev/badge/github.com/yacchi/go-service-start.svg)](https://pkg.go.dev/github.com/yacchi/go-service-start)
[![CI](https://github.com/yacchi/go-service-start/actions/workflows/ci.yml/badge.svg)](https://github.com/yacchi/go-service-start/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/yacchi/go-service-start)](https://goreportcard.com/report/github.com/yacchi/go-service-start)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A small, dependency-free service lifecycle manager for Go applications.

`go-service-start` coordinates the start and shutdown of multiple services
that together form an application — HTTP servers, gRPC servers, queue workers,
schedulers, background loops — with predictable ordering, signal handling,
and error aggregation.

## Features

- **Zero dependencies.** Standard library only.
- **Deterministic ordering.** Higher priority services start first and shut down last.
- **Background services.** Long-running components (e.g. `http.Server.ListenAndServe`) run in a managed goroutine.
- **Signal-aware.** `Wait` traps `SIGINT` / `SIGTERM` and triggers graceful shutdown.
- **Context-driven shutdown.** Optionally cancel a service's start context on shutdown.
- **Startup failure cleanup.** If one service fails to start, the ones already started are shut down in reverse order.
- **Error aggregation.** Background errors and synchronous shutdown errors are joined with `errors.Join`.
- **Restartable.** A manager can be re-used after a completed shutdown.

## Installation

```sh
go get github.com/yacchi/go-service-start
```

Requires Go 1.23 or later (uses `slices.Backward` and `context.WithCancelCause`).

## Quick start

```go
package main

import (
    "context"
    "log"
    "net/http"

    service_start "github.com/yacchi/go-service-start"
)

func main() {
    m := service_start.NewStateManager(nil)

    srv := &http.Server{Addr: ":8080"}
    m.AddService(
        service_start.NewService(
            "http",
            func(ctx context.Context) error {
                // ListenAndServe blocks; mark this service as background.
                if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                    return err
                }
                return nil
            },
            func(ctx context.Context) error { return srv.Shutdown(ctx) },
        ),
        service_start.WithBackground(),
    )

    ctx := context.Background()
    if err := m.Start(ctx); err != nil {
        log.Fatalf("start: %v", err)
    }

    // Block until SIGINT/SIGTERM, then graceful shutdown.
    if sig, err := m.Wait(ctx); err != nil {
        log.Fatalf("shutdown: %v (signal=%v)", err, sig)
    }
}
```

See [`examples/`](examples/) for runnable samples.

## Concepts

### Service

Every managed component implements the `Service` interface:

```go
type Service interface {
    Name() string
    Start(ctx context.Context) error
    Shutdown(ctx context.Context) error
}
```

You can implement it directly on your type, or wrap a pair of closures with
`NewService` when you don't need a dedicated struct.

### Priority

`WithPriority(n)` controls ordering. Services start **highest first** and shut
down in the reverse order. Services with equal priority preserve registration
order. The default priority is 0.

```go
m.AddService(db,    service_start.WithPriority(100)) // started first, stopped last
m.AddService(cache, service_start.WithPriority(50))
m.AddService(api,   service_start.WithPriority(10))  // started last, stopped first
```

### Background services

Some services — typically network listeners — block for their entire lifetime.
Mark them with `WithBackground()` so their `Start` runs in a goroutine. Any
error returned by `Start` is collected and surfaced from `Shutdown`.

```go
m.AddService(httpServer, service_start.WithBackground())
```

A background service's `Shutdown` is still expected to terminate the loop
(e.g. by closing a listener) and return.

### Context-driven shutdown

If your service waits on its start context to stop — for example a worker that
loops on `<-ctx.Done()` — add `WithContextShutdown()`. The manager cancels the
service's context with `service_start.ServiceShutdown` as the cause right
before calling its `Shutdown` method.

```go
m.AddService(worker,
    service_start.WithBackground(),
    service_start.WithContextShutdown(),
)
```

### Lifecycle and state

```
ServiceExited → ServiceStarting → ServiceRunning → ServiceInShutdown → ServiceExited
                       │
                       └─ on start error ─→ ServiceError
```

Inspect the current state with `m.State()`.

### Startup failure

If a synchronous service returns an error from `Start`:

1. The manager stops trying to start further services.
2. Services that were already started are shut down in reverse priority order.
3. `Start` returns a wrapped error joining the original failure with any
   cleanup errors.
4. The manager moves to `ServiceError`.

Background services started before the failure are also shut down; their
errors are aggregated into the returned value.

### Logging

By default the manager logs lifecycle transitions via `log.Printf`. Inject
your own logger (e.g. a `zap`/`slog` adapter) by implementing the `Logger`
interface and passing it via `StateManagerOption`:

```go
m := service_start.NewStateManager(&service_start.StateManagerOption{
    Logger: myLogger{}, // implements Info(format string, v ...interface{})
})
```

Use `service_start.NopLogger()` to silence output entirely.

## API overview

| Symbol | Purpose |
| --- | --- |
| `NewStateManager(opt *StateManagerOption) *ServiceManager` | Construct a manager. `nil` uses defaults. |
| `(*ServiceManager).AddService(svc, opts...)` | Register a service. Must be called before `Start`. |
| `(*ServiceManager).Start(ctx) error` | Start all registered services. |
| `(*ServiceManager).Wait(ctx) (os.Signal, error)` | Block on signal or ctx, then shut down. |
| `(*ServiceManager).Shutdown(ctx) error` | Shut down all started services. |
| `(*ServiceManager).State() ServiceState` | Current lifecycle state. |
| `NewService(name, start, shutdown)` | Create a `Service` from closures. |
| `WithPriority(int)` | Set start/shutdown ordering priority. |
| `WithBackground()` | Run `Start` in a goroutine and aggregate its error. |
| `WithContextShutdown()` | Cancel the service's start context at shutdown. |
| `StandardLogger() / NopLogger()` | Built-in `Logger` implementations. |

Full reference: <https://pkg.go.dev/github.com/yacchi/go-service-start>

## Versioning

This module follows [Semantic Versioning](https://semver.org/). Until a 1.0
release, minor versions may introduce breaking changes; see
[CHANGELOG.md](CHANGELOG.md).

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © Yasunori Fujie
