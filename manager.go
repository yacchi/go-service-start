package service_start

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
)

type container struct {
	Service
	priority   int
	background bool
}

type ServiceOption func(c *container)

func WithPriority(priority int) ServiceOption {
	return func(c *container) {
		c.priority = priority
	}
}

func WithBackground() ServiceOption {
	return func(c *container) {
		c.background = true
	}
}

type ServiceManager struct {
	state    ServiceState
	wgStop   sync.WaitGroup
	services []*container
	logger   Logger
}

type StateManagerOption struct {
	Logger Logger
}

func NewStateManagerOption() *StateManagerOption {
	return &StateManagerOption{
		Logger: StandardLogger(),
	}
}

func NewStateManager(option *StateManagerOption) *ServiceManager {
	if option.Logger == nil {
		option.Logger = StandardLogger()
	}
	return &ServiceManager{
		logger: option.Logger,
	}
}

func (s *ServiceManager) State() ServiceState {
	return s.state
}

func (s *ServiceManager) AddService(svc Service, opts ...ServiceOption) {
	c := &container{Service: svc}
	for _, opt := range opts {
		opt(c)
	}
	s.services = append(s.services, c)

	sort.SliceStable(s.services, func(i, j int) bool {
		return s.services[i].priority > s.services[j].priority
	})
}

func (s *ServiceManager) Start(ctx context.Context) (err error) {
	s.wgStop = sync.WaitGroup{}
	s.state = ServiceStarting

	for _, svc := range s.services {
		s.logger.Info("service_start: start '%s'", svc.Name())

		if svc.background {
			go func(svc Service) {
				defer func() {
					s.wgStop.Done()
					s.logger.Info("service_start: '%s' stopped", svc.Name())
				}()
				if e := svc.Start(ctx); e != nil {
					err = errors.Join(err, e)
				}
			}(svc)
			s.wgStop.Add(1)
		} else {
			if e := svc.Start(ctx); e != nil {
				err = errors.Join(err, e)
			}
		}
	}

	if err != nil {
		return fmt.Errorf("service_start: startup error: %w", err)
	}

	s.state = ServiceRunning

	return nil
}

func (s *ServiceManager) Wait(ctx context.Context) (os.Signal, error) {
	var sig os.Signal

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer close(c)

	select {
	case <-c:
	case <-ctx.Done():
	}

	err := s.Shutdown(ctx)
	return sig, err
}

func (s *ServiceManager) Shutdown(ctx context.Context) (err error) {
	s.state = ServiceInShutdown

	for i := len(s.services) - 1; 0 <= i; i-- {
		svc := s.services[i]
		name := svc.Name()
		s.logger.Info("service_start: shutdown '%s'", name)
		if e := svc.Shutdown(ctx); e != nil {
			err = errors.Join(err, e)
		}
	}

	s.wgStop.Wait()

	s.state = ServiceExited
	return
}
