package workflow

import (
	"context"
	"errors"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type Stage string
type EventKind string

const (
	EventStarted   EventKind = "STARTED"
	EventProgress  EventKind = "PROGRESS"
	EventCompleted EventKind = "COMPLETED"
	EventFailed    EventKind = "FAILED"
)

type Event struct {
	Stage      Stage
	Kind       EventKind
	Operation  string
	Message    string
	Current    int64
	Total      int64
	Attributes map[string]string
	Time       time.Time
}

type Observer interface {
	OnEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (f ObserverFunc) OnEvent(ctx context.Context, event Event) {
	if f != nil {
		f(ctx, event)
	}
}

type Step[S any] interface {
	Name() Stage
	Run(context.Context, S) error
}

type Pipeline[S any] struct {
	observer Observer
	steps    []Step[S]
	now      func() time.Time
}

func NewPipeline[S any](observer Observer, steps ...Step[S]) *Pipeline[S] {
	copied := append([]Step[S](nil), steps...)
	return &Pipeline[S]{observer: observer, steps: copied, now: time.Now}
}

func (p *Pipeline[S]) Run(ctx context.Context, state S) error {
	if ctx == nil || p == nil {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "workflow.run", Cause: errors.New("nil context or pipeline")}
	}
	for _, step := range p.steps {
		if step == nil {
			return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "workflow.run", Cause: errors.New("nil workflow step")}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		p.emit(ctx, Event{Stage: step.Name(), Kind: EventStarted})
		if err := step.Run(ctx, state); err != nil {
			p.emit(ctx, Event{Stage: step.Name(), Kind: EventFailed, Message: "stage failed"})
			return err
		}
		p.emit(ctx, Event{Stage: step.Name(), Kind: EventCompleted})
	}
	return nil
}

func (p *Pipeline[S]) emit(ctx context.Context, event Event) {
	if p == nil || p.observer == nil {
		return
	}
	event.Time = p.now()
	if event.Attributes != nil {
		event.Attributes = cloneAttributes(event.Attributes)
	}
	p.observer.OnEvent(ctx, event)
}

func cloneAttributes(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
