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
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (s ServiceA) Shutdown(ctx context.Context) error {
	time.Sleep(10 * time.Millisecond)
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

	ctx2, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
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

	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
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

// TestStartErrorCleanup は同期サービスの Start 失敗時、それまでに起動した
// サービスのみが優先度の逆順で Shutdown され、未起動のサービスの Shutdown は
// 呼ばれず、状態が ServiceError になることを検証する。
func TestStartErrorCleanup(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})

	var events []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, s)
	}

	// 優先度 30 (最先) → OK
	m.AddService(NewService("first",
		func(ctx context.Context) error { record("start:first"); return nil },
		func(ctx context.Context) error { record("shutdown:first"); return nil },
	), WithPriority(30))

	// 優先度 20 → 起動失敗
	startErr := errors.New("boom")
	m.AddService(NewService("middle",
		func(ctx context.Context) error { record("start:middle"); return startErr },
		func(ctx context.Context) error { record("shutdown:middle"); return nil },
	), WithPriority(20))

	// 優先度 10 → 呼ばれないはず
	m.AddService(NewService("last",
		func(ctx context.Context) error { record("start:last"); return nil },
		func(ctx context.Context) error { record("shutdown:last"); return nil },
	), WithPriority(10))

	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start error")
	}
	if !errors.Is(err, startErr) {
		t.Errorf("expected start error wrapped, got: %v", err)
	}
	if got := m.State(); got != ServiceError {
		t.Errorf("expected state ServiceError, got %s", got)
	}

	want := []string{"start:first", "start:middle", "shutdown:first"}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != len(want) {
		t.Fatalf("events mismatch: got %v want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event[%d]: got %q want %q", i, events[i], want[i])
		}
	}
}

// TestShutdownOrderByPriority は優先度の高い順に Start され、Shutdown は
// 逆順で呼ばれることを検証する。
func TestShutdownOrderByPriority(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})

	var events []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, s)
	}

	mk := func(name string, prio int) {
		m.AddService(NewService(name,
			func(ctx context.Context) error { record("start:" + name); return nil },
			func(ctx context.Context) error { record("shutdown:" + name); return nil },
		), WithPriority(prio))
	}
	mk("low", 1)
	mk("high", 10)
	mk("mid", 5)

	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"start:high", "start:mid", "start:low",
		"shutdown:low", "shutdown:mid", "shutdown:high",
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != len(want) {
		t.Fatalf("got %v want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event[%d]: got %q want %q", i, events[i], want[i])
		}
	}
}

// TestStateTransitions は State() が各遷移点で期待値を返すことを検証する。
func TestStateTransitions(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})

	if got := m.State(); got != ServiceExited {
		t.Errorf("initial: got %s want ServiceExited", got)
	}

	gate := make(chan struct{})
	observedDuringStart := make(chan ServiceState, 1)
	m.AddService(NewService("svc",
		func(ctx context.Context) error {
			observedDuringStart <- m.State()
			<-gate
			return nil
		},
		func(ctx context.Context) error { return nil },
	))

	go func() {
		_ = m.Start(context.Background())
	}()
	select {
	case got := <-observedDuringStart:
		if got != ServiceStarting {
			t.Errorf("during Start: got %s want ServiceStarting", got)
		}
	case <-time.After(time.Second):
		t.Fatal("service did not start in time")
	}

	close(gate)
	// Start returns → ServiceRunning
	deadline := time.Now().Add(time.Second)
	for m.State() != ServiceRunning {
		if time.Now().After(deadline) {
			t.Fatalf("expected ServiceRunning, got %s", m.State())
		}
		time.Sleep(time.Millisecond)
	}

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.State(); got != ServiceExited {
		t.Errorf("after Shutdown: got %s want ServiceExited", got)
	}
}

// TestAddServicePanicsAfterStart は Start 中の AddService が panic することを検証する。
func TestAddServicePanicsAfterStart(t *testing.T) {
	m := NewStateManager(&StateManagerOption{Logger: NopLogger()})
	m.AddService(NewService("svc",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	))
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown(context.Background())

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when AddService called after Start")
		}
	}()
	m.AddService(NewService("late",
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	))
}
