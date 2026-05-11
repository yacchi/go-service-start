// Command http demonstrates running an HTTP server alongside a background
// worker using go-service-start. Run it and press Ctrl-C to trigger a graceful
// shutdown.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	service_start "github.com/yacchi/go-service-start"
)

func main() {
	m := service_start.NewStateManager(nil)

	// A background worker that loops until its context is cancelled.
	m.AddService(
		service_start.NewService(
			"ticker",
			func(ctx context.Context) error {
				t := time.NewTicker(time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case now := <-t.C:
						log.Printf("tick %s", now.Format(time.RFC3339))
					}
				}
			},
			func(ctx context.Context) error { return nil },
		),
		service_start.WithBackground(),
		service_start.WithContextShutdown(),
		service_start.WithPriority(10),
	)

	// An HTTP server. Started after the worker, shut down before it.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from go-service-start\n"))
	})
	srv := &http.Server{Addr: ":8080", Handler: mux}

	m.AddService(
		service_start.NewService(
			"http",
			func(ctx context.Context) error {
				log.Printf("listening on %s", srv.Addr)
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			},
			func(ctx context.Context) error { return srv.Shutdown(ctx) },
		),
		service_start.WithBackground(),
		service_start.WithPriority(1),
	)

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}
	sig, err := m.Wait(ctx)
	if err != nil {
		log.Fatalf("shutdown: %v (signal=%v)", err, sig)
	}
	log.Printf("exited (signal=%v)", sig)
}
