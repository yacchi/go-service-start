package service_start

import (
	"context"
	"errors"
)

//go:generate stringer -type=ServiceState service_state.go

type ServiceState int

const (
	ServiceExited ServiceState = iota
	ServiceStarting
	ServiceRunning
	ServiceInShutdown
	ServiceError
)

type Logger interface {
	Info(format string, v ...interface{})
}

type Service interface {
	Name() string
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

var ServiceShutdown = errors.New("service is shutting down")
