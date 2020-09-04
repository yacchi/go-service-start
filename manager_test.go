package service_start

import (
	"context"
	"fmt"
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

func (d *DaemonB) Name() string {
	return "DaemonB"
}

func (d *DaemonB) Start(ctx context.Context) error {
	d.c = make(chan struct{})
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

	daemonB := &DaemonB{}
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
