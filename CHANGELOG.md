# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.3.0]

- Startup failure cleanup and API hardening; module targets Go 1.23 (uses `slices.Backward` and `context.WithCancelCause`).
- `AddService` now panics if called while the manager is starting, running, or shutting down.

## [0.2.0]

- Context-based shutdown via `WithContextShutdown`.
- Race fix between `WaitGroup` and the named return value `err` in background error aggregation.

## [0.1.0]

- Initial release: `ServiceManager` with priority-ordered start/shutdown, background services, signal-driven `Wait`, and a pluggable `Logger`.

[0.3.0]: https://github.com/yacchi/go-service-start/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/yacchi/go-service-start/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/yacchi/go-service-start/releases/tag/v0.1.0
