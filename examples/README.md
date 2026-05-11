# Examples

Runnable samples for `go-service-start`.

| Example | Description |
| --- | --- |
| [`http`](./http) | HTTP server (background) plus a ticker worker that exits on context cancellation. |

Run an example from the repository root:

```sh
go run ./examples/http
```

Press Ctrl-C to exercise the graceful shutdown path.
