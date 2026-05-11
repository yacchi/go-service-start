package service_start

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type ServiceA struct {
}

func (s ServiceA) Name() string {
	return "A"
}

func (s ServiceA) Start(ctx context.Context) error {
	time.Sleep(time.Second * 5)
	return nil
}

func (s ServiceA) Shutdown(ctx context.Context) error {
	time.Sleep(time.Second * 5)
	return nil
}

type DaemonB struct {
	c chan struct{}
}

func newDaemonB() *DaemonB {
	return &DaemonB{c: make(chan struct{})}
}

func (d *DaemonB) Name() string {
	return "DaemonB"
}

func (d *DaemonB) Start(ctx context.Context) error {
	<-d.c
	return nil
}

func (d *DaemonB) Shutdown(ctx context.Context) error {
	close(d.c)
	return nil
}

func TestManager(t *testing.T) {
	m := NewStateManager(NewStateManagerOption())

	svcA := &ServiceA{}
	m.AddService(svcA)

	daemonB := newDaemonB()
	m.AddService(daemonB, WithBackground(), WithPriority(10))

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}

	ctx2, _ := context.WithTimeout(ctx, time.Second*2)
	if sig, err := m.Wait(ctx2); err != nil {
		t.Fatal(err)
	} else {
		t.Log(sig)
	}
}

func TestNewService(t *testing.T) {
	m := NewStateManager(NewStateManagerOption())

	start := func(ctx context.Context) error {
		fmt.Println("start")
		return nil
	}

	shutdown := func(ctx context.Context) error {
		fmt.Println("shutdown")
		return nil
	}

	m.AddService(NewService("service", start, shutdown))

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}

	ctx2, _ := context.WithTimeout(context.Background(), time.Second)
	if sig, err := m.Wait(ctx2); err != nil {
		t.Fatal(err)
	} else {
		t.Log(sig)
	}
}

// TestBackgroundServiceImmediateReturn は background サービスが Add(1) 前に Done() を
// 呼んでしまう WaitGroup 競合を検出する。Start() を即座にリターンする実装で、もし
// `go func()` → `wgStop.Add(1)` の順序のままだと、稀に「negative WaitGroup counter」
// パニックが起きる。修正後の実装ではこの順序が逆転しているのでパニックは起きない。
func TestBackgroundServiceImmediateReturn(t *testing.T) {
	for i := 0; i < 100; i++ {
		m := NewStateManager(&StateManagerOption{Logger: NopLogger()})
		for j := 0; j < 8; j++ {
			start := func(ctx context.Context) error { return nil }
			shutdown := func(ctx context.Context) error { return nil }
			m.AddService(NewService(fmt.Sprintf("svc-%d", j), start, shutdown), WithBackground())
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatalf("iteration %d: Start failed: %v", i, err)
		}
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("iteration %d: Shutdown failed: %v", i, err)
		}
	}
}

// TestBackgroundServiceErrorAggregation は background サービスから返るエラーが
// 競合なく収集され、Shutdown の戻り値に集約されることを検証する。
// 修正前は名前付き戻り値 err を複数 goroutine が同時に書き込んでいた。
func TestBackgroundServiceErrorAggregation(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})

	const n = 16
	var startWG sync.WaitGroup
	startWG.Add(n)
	expectedErrs := make([]error, n)
	for i := 0; i < n; i++ {
		e := fmt.Errorf("bg-err-%d", i)
		expectedErrs[i] = e
		start := func(ctx context.Context) error {
			startWG.Done()
			startWG.Wait() // 全 goroutine が同時にエラーを返す
			return e
		}
		shutdown := func(ctx context.Context) error { return nil }
		m.AddService(NewService(fmt.Sprintf("svc-%d", i), start, shutdown), WithBackground())
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	startWG.Wait()
	err := m.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected aggregated background errors, got nil")
	}
	for _, want := range expectedErrs {
		if !errors.Is(err, want) {
			t.Errorf("missing background error in aggregation: %v", want)
		}
	}
}

// TestContextShutdownPropagates は WithContextShutdown 付きのサービスが、
// Shutdown 時に context cancel で抜けられることを検証する。
func TestContextShutdownPropagates(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})

	var started atomic.Bool
	start := func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done()
		return ctx.Err()
	}
	shutdown := func(ctx context.Context) error { return nil }
	m.AddService(NewService("ctx-svc", start, shutdown), WithBackground(), WithContextShutdown())

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for !started.Load() {
		if time.Now().After(deadline) {
			t.Fatal("service did not start in time")
		}
		time.Sleep(time.Millisecond)
	}

	done := make(chan struct{})
	go func() {
		_ = m.Shutdown(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not return; context cancel likely did not propagate")
	}
}

// TestStartShutdownRepeated は同一マネージャーを Start → Shutdown を繰り返した際に
// WaitGroup と bgErrs が再利用されても安全であることを検証する。
func TestStartShutdownRepeated(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})
	m.AddService(NewService("bg",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	), WithBackground())

	for i := 0; i < 5; i++ {
		if err := m.Start(context.Background()); err != nil {
			t.Fatalf("iteration %d: Start: %v", i, err)
		}
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("iteration %d: Shutdown: %v", i, err)
		}
	}
}
