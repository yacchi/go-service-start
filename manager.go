package service_start

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
)

type container struct {
	Service
	ctx         context.Context
	cancel      context.CancelCauseFunc
	priority    int
	background  bool
	stopContext bool
	started     bool
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
	state    atomic.Int32
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
	if option == nil {
		option = NewStateManagerOption()
	}
	if option.Logger == nil {
		option.Logger = StandardLogger()
	}
	return &ServiceManager{
		logger: option.Logger,
	}
}

func (s *ServiceManager) State() ServiceState {
	return ServiceState(s.state.Load())
}

func (s *ServiceManager) setState(state ServiceState) {
	s.state.Store(int32(state))
}

// AddService registers svc with the manager. It must be called before Start
// (or after a completed Shutdown), and is not safe for concurrent use.
// Calling it while the manager is in ServiceStarting, ServiceRunning, or
// ServiceInShutdown panics.
func (s *ServiceManager) AddService(svc Service, opts ...ServiceOption) {
	switch state := s.State(); state {
	case ServiceStarting, ServiceRunning, ServiceInShutdown:
		panic(fmt.Sprintf("service_start: AddService called in state %s", state))
	}

	c := &container{Service: svc}
	for _, opt := range opts {
		opt(c)
	}
	s.services = append(s.services, c)

	slices.SortStableFunc(s.services, func(a, b *container) int {
		return cmp.Compare(b.priority, a.priority)
	})
}

func (s *ServiceManager) Start(ctx context.Context) error {
	s.wgStop = sync.WaitGroup{}
	s.bgErrs = nil
	s.setState(ServiceStarting)

	var startErr error
	for _, svc := range s.services {
		s.logger.Info("service_start: start '%s'", svc.Name())
		svcCtx, cancel := context.WithCancelCause(ctx)
		svc.ctx = svcCtx
		svc.cancel = cancel

		if svc.background {
			s.wgStop.Add(1)
			svc.started = true
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
				startErr = e
				break
			}
			svc.started = true
		}
	}

	if startErr != nil {
		cleanupErr := s.shutdownStarted(ctx)
		s.setState(ServiceError)
		return fmt.Errorf("service_start: startup error: %w", errors.Join(startErr, cleanupErr))
	}

	s.setState(ServiceRunning)
	return nil
}

// Wait blocks until a termination signal (SIGINT, SIGTERM, os.Interrupt) is
// received or ctx is cancelled, then calls Shutdown. The returned signal is
// non-nil only when triggered by a signal; if it is nil, Wait returned
// because ctx was cancelled.
func (s *ServiceManager) Wait(ctx context.Context) (os.Signal, error) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(c)

	var sig os.Signal
	select {
	case sig = <-c:
	case <-ctx.Done():
	}

	err := s.Shutdown(ctx)
	return sig, err
}

func (s *ServiceManager) Shutdown(ctx context.Context) error {
	s.setState(ServiceInShutdown)
	err := s.shutdownStarted(ctx)
	s.setState(ServiceExited)
	return err
}

// shutdownStarted shuts down only services that were actually started, in
// reverse priority order, and aggregates errors from sync Shutdown calls and
// background Start goroutines.
func (s *ServiceManager) shutdownStarted(ctx context.Context) error {
	var err error

	for _, svc := range slices.Backward(s.services) {
		if !svc.started {
			continue
		}
		s.logger.Info("service_start: shutdown '%s'", svc.Name())
		if svc.stopContext {
			svc.cancel(ServiceShutdown)
		}
		if e := svc.Shutdown(ctx); e != nil {
			err = errors.Join(err, e)
		}
		svc.started = false
	}

	s.wgStop.Wait()

	s.mu.Lock()
	for _, e := range s.bgErrs {
		err = errors.Join(err, e)
	}
	s.bgErrs = nil
	s.mu.Unlock()

	return err
}
