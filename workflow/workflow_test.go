package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type testState struct{ Values []string }

type testStep struct {
	name  Stage
	value string
}

func (s testStep) Name() Stage { return s.name }

func (s testStep) Run(ctx context.Context, state *testState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state.Values = append(state.Values, s.value)
	return nil
}

type failingStep struct {
	name Stage
	err  error
}

func (s failingStep) Name() Stage { return s.name }

func (s failingStep) Run(context.Context, *testState) error { return s.err }

func TestPipelineRunsInOrder(t *testing.T) {
	state := &testState{}
	var kinds []EventKind
	p := NewPipeline[*testState](
		ObserverFunc(func(_ context.Context, event Event) {
			kinds = append(kinds, event.Kind)
		}),
		testStep{name: "one", value: "1"},
		testStep{name: "two", value: "2"},
	)
	if err := p.Run(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.Values, []string{"1", "2"}) {
		t.Fatalf("values = %#v", state.Values)
	}
	wantKinds := []EventKind{EventStarted, EventCompleted, EventStarted, EventCompleted}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("events = %#v", kinds)
	}
}

func TestPipelineStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := NewPipeline[*testState](nil, testStep{name: "one", value: "1"})
	err := p.Run(ctx, &testState{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestPipelineFailureEventUsesSafeMessage(t *testing.T) {
	stepErr := errors.New("private-key=step-secret")
	var events []Event
	p := NewPipeline[*testState](
		ObserverFunc(func(_ context.Context, event Event) {
			events = append(events, event)
		}),
		failingStep{name: "one", err: stepErr},
	)

	err := p.Run(t.Context(), &testState{})
	if !errors.Is(err, stepErr) {
		t.Fatalf("error = %v", err)
	}
	wantKinds := []EventKind{EventStarted, EventFailed}
	var kinds []EventKind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("events = %#v", kinds)
	}
	if got := events[1].Message; got != "stage failed" {
		t.Fatalf("failure message = %q", got)
	}
}

func TestPipelineRejectsNilContext(t *testing.T) {
	err := NewPipeline[*testState](nil).Run(nil, &testState{})
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("error=%#v", err)
	}
}
