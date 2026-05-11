// Package service_start provides a small, dependency-free service lifecycle
// manager for Go applications.
//
// A ServiceManager coordinates the start and shutdown of multiple services
// that together make up an application (HTTP servers, queue workers, schedulers,
// background loops, etc.). Services are registered with optional priorities
// and behavior flags, started in priority order, and shut down in reverse
// order on signal, context cancellation, or an explicit call.
//
// # Quick start
//
//	m := service_start.NewStateManager(nil)
//
//	m.AddService(service_start.NewService(
//	    "http",
//	    func(ctx context.Context) error { return httpServer.Start() },
//	    func(ctx context.Context) error { return httpServer.Shutdown(ctx) },
//	))
//
//	if err := m.Start(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//	if _, err := m.Wait(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
// # Concepts
//
// Service is the interface every managed component implements. Implement it
// directly, or wrap a pair of start/shutdown closures with NewService.
//
// Priority controls ordering. Higher priority services start first and shut
// down last. Services with equal priority preserve registration order. Use
// WithPriority to set it.
//
// Background services keep running until shut down. Their Start is called in
// a goroutine and is expected to block (e.g. http.Server.ListenAndServe).
// Errors returned from Start are aggregated and surfaced from Shutdown.
// Use WithBackground.
//
// Context-driven shutdown cancels a service's start context when Shutdown
// begins. Useful for goroutines that wait on <-ctx.Done() instead of
// implementing a separate stop channel. Use WithContextShutdown.
//
// # Lifecycle
//
// The manager moves through these states (see ServiceState):
//
//	ServiceExited → ServiceStarting → ServiceRunning → ServiceInShutdown → ServiceExited
//
// If a synchronous service returns an error from Start, already-started
// services are shut down in reverse priority order and the manager
// transitions to ServiceError. After a completed Shutdown the manager
// returns to ServiceExited and may be started again.
package service_start
