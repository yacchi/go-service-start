package service_start

import "context"

type service struct {
	name            string
	start, shutdown func(ctx context.Context) error
}

func (s *service) Name() string {
	return s.name
}

func (s *service) Start(ctx context.Context) error {
	if s.start != nil {
		return s.start(ctx)
	}
	return nil
}

func (s *service) Shutdown(ctx context.Context) error {
	if s.shutdown != nil {
		return s.shutdown(ctx)
	}
	return nil
}

func NewService(name string, start, shutdown func(ctx context.Context) error) Service {
	return &service{
		name:     name,
		start:    start,
		shutdown: shutdown,
	}
}
