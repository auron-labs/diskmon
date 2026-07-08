package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"diskmon/internal/health"
)

type fakeFactory struct {
	senders map[string]*fakeSender
}

func (f fakeFactory) Build(entry Entry) (Sender, error) {
	s, ok := f.senders[entry.Name]
	if !ok {
		s = &fakeSender{}
	}
	return s, nil
}

type fakeSender struct {
	calls   int
	subject string
	body    string
	err     error
}

func (f *fakeSender) Send(_ context.Context, subject, body string) error {
	f.calls++
	f.subject = subject
	f.body = body
	return f.err
}

func TestDispatchIfNeeded_InitialPassSuppressed(t *testing.T) {
	sender := &fakeSender{}
	entry := Entry{Name: "ops", Enabled: true, OnPass: true, OnFail: true}
	d, err := NewDispatcher([]Entry{entry}, fakeFactory{senders: map[string]*fakeSender{"ops": sender}}, time.Second)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	res, err := d.DispatchIfNeeded(context.Background(), DispatchRequest{
		DriveID:        "/dev/sda",
		PreviousStatus: nil,
		Current:        health.Result{Status: health.StatusGreen, Score: 90},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected 0 sends, got %d", sender.calls)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Reason != ReasonInitialPassSuppressed {
		t.Fatalf("unexpected outcome: %+v", res.Outcomes)
	}
}

func TestDispatchIfNeeded_InitialFailSends(t *testing.T) {
	sender := &fakeSender{}
	entry := Entry{Name: "ops", Enabled: true, OnPass: true, OnFail: true}
	d, err := NewDispatcher([]Entry{entry}, fakeFactory{senders: map[string]*fakeSender{"ops": sender}}, time.Second)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	res, err := d.DispatchIfNeeded(context.Background(), DispatchRequest{
		DriveID:        "/dev/sda",
		PreviousStatus: nil,
		Current:        health.Result{Status: health.StatusRed, Score: 20, Reasons: []string{"SMART_OVERALL_FAILED"}},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("expected 1 send, got %d", sender.calls)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Reason != ReasonInitialFail || !res.Outcomes[0].Sent {
		t.Fatalf("unexpected outcome: %+v", res.Outcomes)
	}
}

func TestDispatchIfNeeded_TransitionSends(t *testing.T) {
	prevRed := health.StatusRed
	prevGreen := health.StatusGreen

	tests := []struct {
		name     string
		previous *health.Status
		current  health.Status
		onPass   bool
		onFail   bool
		reason   DecisionReason
	}{
		{
			name:     "transition to fail sends",
			previous: &prevGreen,
			current:  health.StatusYellow,
			onPass:   true,
			onFail:   true,
			reason:   ReasonTransitionToFail,
		},
		{
			name:     "transition to pass sends",
			previous: &prevRed,
			current:  health.StatusGreen,
			onPass:   true,
			onFail:   true,
			reason:   ReasonTransitionToPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			entry := Entry{Name: "ops", Enabled: true, OnPass: tt.onPass, OnFail: tt.onFail}
			d, err := NewDispatcher([]Entry{entry}, fakeFactory{senders: map[string]*fakeSender{"ops": sender}}, time.Second)
			if err != nil {
				t.Fatalf("new dispatcher: %v", err)
			}

			res, err := d.DispatchIfNeeded(context.Background(), DispatchRequest{
				DriveID:        "/dev/sda",
				PreviousStatus: tt.previous,
				Current:        health.Result{Status: tt.current, Score: 50},
			})
			if err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if sender.calls != 1 {
				t.Fatalf("expected 1 send, got %d", sender.calls)
			}
			if len(res.Outcomes) != 1 || res.Outcomes[0].Reason != tt.reason || !res.Outcomes[0].Sent {
				t.Fatalf("unexpected outcome: %+v", res.Outcomes)
			}
		})
	}
}

func TestDispatchIfNeeded_UnchangedSuppressed(t *testing.T) {
	prev := health.StatusYellow
	sender := &fakeSender{}
	entry := Entry{Name: "ops", Enabled: true, OnPass: true, OnFail: true}
	d, err := NewDispatcher([]Entry{entry}, fakeFactory{senders: map[string]*fakeSender{"ops": sender}}, time.Second)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	res, err := d.DispatchIfNeeded(context.Background(), DispatchRequest{
		DriveID:        "/dev/sda",
		PreviousStatus: &prev,
		Current:        health.Result{Status: health.StatusYellow, Score: 65},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected 0 sends, got %d", sender.calls)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Reason != ReasonUnchanged {
		t.Fatalf("unexpected outcome: %+v", res.Outcomes)
	}
}

func TestDispatchIfNeeded_DisabledEntrySuppressed(t *testing.T) {
	sender := &fakeSender{}
	entry := Entry{Name: "ops", Enabled: false, OnPass: true, OnFail: true}
	d, err := NewDispatcher([]Entry{entry}, fakeFactory{senders: map[string]*fakeSender{"ops": sender}}, time.Second)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	res, err := d.DispatchIfNeeded(context.Background(), DispatchRequest{
		DriveID:        "/dev/sda",
		PreviousStatus: nil,
		Current:        health.Result{Status: health.StatusRed, Score: 20},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected 0 sends, got %d", sender.calls)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Reason != ReasonDisabled {
		t.Fatalf("unexpected outcome: %+v", res.Outcomes)
	}
}

func TestDispatchIfNeeded_SendFailureIsolatedPerEntry(t *testing.T) {
	prev := health.StatusGreen
	failing := &fakeSender{err: errors.New("boom")}
	ok := &fakeSender{}
	entries := []Entry{
		{Name: "a", Enabled: true, OnPass: true, OnFail: true},
		{Name: "b", Enabled: true, OnPass: true, OnFail: true},
	}
	d, err := NewDispatcher(entries, fakeFactory{senders: map[string]*fakeSender{"a": failing, "b": ok}}, time.Second)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	res, err := d.DispatchIfNeeded(context.Background(), DispatchRequest{
		DriveID:        "/dev/sda",
		PreviousStatus: &prev,
		Current:        health.Result{Status: health.StatusRed, Score: 20},
	})
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if failing.calls != 1 || ok.calls != 1 {
		t.Fatalf("expected both sends attempted, got failing=%d ok=%d", failing.calls, ok.calls)
	}
	if len(res.Outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(res.Outcomes))
	}
	if !res.Outcomes[0].Attempted || res.Outcomes[0].Sent || res.Outcomes[0].Err == nil {
		t.Fatalf("expected failed outcome contract Attempted=true Sent=false Err!=nil, got %+v", res.Outcomes[0])
	}
	if !res.Outcomes[1].Sent {
		t.Fatalf("expected second outcome success, got %+v", res.Outcomes[1])
	}
}
