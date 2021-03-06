package routines

import (
	"fmt"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	"sync"
	"time"
)

type RoutineHandler interface {
	Init() error
	Poll() error
	Cleanup() error
}

type Event struct {
	//http status code: 0 means does not affect status code
	Status  int
	Message string
}

type Routine struct {
	Events <-chan *Event

	mtx          sync.Mutex
	started      bool
	terminated   bool
	client       *kclient.Client
	events       chan<- *Event
	pollInterval time.Duration
	podTTL       time.Duration
	handler      RoutineHandler
}

func NewRoutine(client *kclient.Client,
	pollInterval time.Duration,
	podTTL time.Duration,
	handler RoutineHandler,
) *Routine {
	events := make(chan *Event)

	return &Routine{
		Events:       events,
		events:       events,
		client:       client,
		pollInterval: pollInterval,
		podTTL:       podTTL,
		handler:      handler,
	}
}
func (r *Routine) Start() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.started {
		return fmt.Errorf("Routine is already started")
	}
	r.started = true
	go r.routine()
	return nil
}

func (r *Routine) SignalTerminate() {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.terminated = true
}

const pulseInterval = 2 * time.Second

func (r *Routine) routine() {
	defer func() {
		r.events <- nil
	}()

	for !r.terminated {
		if err := r.handler.Init(); err != nil {
			r.events <- &Event{
				Status:  500,
				Message: fmt.Sprintf("init error: %s", err),
			}
			time.Sleep(pulseInterval)
			continue
		} else {
			r.events <- &Event{
				Status:  0,
				Message: "init success",
			}
		}

		lastPoll := time.Unix(0, 0)
		podStart := time.Now()
		for !r.terminated && time.Since(podStart) <= r.podTTL {
			if time.Since(lastPoll) > r.pollInterval {

				if err := r.handler.Poll(); err != nil {
					r.events <- &Event{
						Status:  500,
						Message: fmt.Sprintf("poll error: %s", err.Error()),
					}
				} else {
					r.events <- &Event{
						Status:  200,
						Message: "poll success",
					}
				}

				lastPoll = time.Now()
			}
			time.Sleep(pulseInterval)
		}
		r.events <- &Event{
			Message: "terminated, will clean up",
		}

		for {
			if err := r.handler.Cleanup(); err != nil {
				r.events <- &Event{
					Status:  500,
					Message: fmt.Sprintf("cleanup error: %s", err),
				}
			} else {
				r.events <- &Event{
					Message: "cleanup success",
				}
				break
			}
			time.Sleep(pulseInterval)
		}
	}
}
