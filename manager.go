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
	ctx         context.Context
	cancel      context.CancelCauseFunc
	priority    int
	background  bool
	stopContext bool
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

func WithContextShutdown() ServiceOption {
	return func(c *container) {
		c.stopContext = true
	}
}

type ServiceManager struct {
	mu       sync.Mutex
	state    ServiceState
	wgStop   sync.WaitGroup
	services []*container
	logger   Logger
	bgErrs   []error
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
	s.bgErrs = nil
	s.state = ServiceStarting

	for _, svc := range s.services {
		s.logger.Info("service_start: start '%s'", svc.Name())
		svcCtx, cancel := context.WithCancelCause(ctx)
		svc.ctx = svcCtx
		svc.cancel = cancel

		if svc.background {
			s.wgStop.Add(1)
			go func(svc *container) {
				defer func() {
					s.logger.Info("service_start: '%s' stopped", svc.Name())
					s.wgStop.Done()
				}()
				if e := svc.Start(svcCtx); e != nil {
					s.mu.Lock()
					s.bgErrs = append(s.bgErrs, e)
					s.mu.Unlock()
				}
			}(svc)
		} else {
			if e := svc.Start(svcCtx); e != nil {
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
		if svc.stopContext {
			svc.cancel(ServiceShutdown)
		}
		if e := svc.Shutdown(ctx); e != nil {
			err = errors.Join(err, e)
		}
	}

	s.wgStop.Wait()

	s.mu.Lock()
	for _, e := range s.bgErrs {
		err = errors.Join(err, e)
	}
	s.bgErrs = nil
	s.mu.Unlock()

	s.state = ServiceExited
	return
}
