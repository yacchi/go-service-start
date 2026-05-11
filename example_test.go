package service_start_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	service_start "github.com/yacchi/go-service-start"
)

func ExampleNewService() {
	m := service_start.NewStateManager(&service_start.StateManagerOption{
		Logger: service_start.NopLogger(),
	})

	m.AddService(service_start.NewService(
		"greeter",
		func(ctx context.Context) error { fmt.Println("started"); return nil },
		func(ctx context.Context) error { fmt.Println("stopped"); return nil },
	))

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		fmt.Println(err)
		return
	}
	if err := m.Shutdown(ctx); err != nil {
		fmt.Println(err)
	}
	// Output:
	// started
	// stopped
}

func ExampleWithBackground() {
	srv := &http.Server{Addr: "127.0.0.1:0"}
	m := service_start.NewStateManager(&service_start.StateManagerOption{
		Logger: service_start.NopLogger(),
	})

	m.AddService(
		service_start.NewService(
			"http",
			func(ctx context.Context) error {
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
		fmt.Println(err)
		return
	}
	if err := m.Shutdown(ctx); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("done")
	// Output: done
}
