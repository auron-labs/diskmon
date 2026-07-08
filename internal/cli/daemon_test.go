package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"diskmon/internal/health"
	"diskmon/internal/notification"
	"diskmon/internal/smart"
	"diskmon/internal/storage"
)

func TestRunCollectionCycle_MultiDriveMultiEntryTransitions(t *testing.T) {
	timestamp := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	collector := &fakeCollector{
		results: []smart.CollectResult{
			{Info: smart.DriveInfo{Device: "/dev/sda"}, Sample: smart.SmartSample{CollectedAt: timestamp, RawJSON: "/dev/sda"}},
			{Info: smart.DriveInfo{Device: "/dev/sdb"}, Sample: smart.SmartSample{CollectedAt: timestamp, RawJSON: "/dev/sdb"}},
		},
	}
	evaluator := &fakeEvaluator{
		byDevice: map[string]health.Result{
			"/dev/sda": {Status: health.StatusYellow, Score: 65},
			"/dev/sdb": {Status: health.StatusGreen, Score: 95},
		},
	}
	store := &fakeDaemonStore{
		driveIDByDevice: map[string]int64{
			"/dev/sda": 1,
			"/dev/sdb": 2,
		},
		states: map[string]string{
			stateKey(1, "ops"):   string(health.StatusGreen),
			stateKey(1, "audit"): string(health.StatusYellow),
			stateKey(2, "audit"): string(health.StatusGreen),
		},
	}
	events := &fakeEventPublisher{}

	opsSender := &fakeNotificationSender{}
	auditSender := &fakeNotificationSender{}
	targets := []notificationTarget{
		buildTestTarget(t, notification.Entry{Name: "ops", Enabled: true, OnPass: true, OnFail: true}, opsSender),
		buildTestTarget(t, notification.Entry{Name: "audit", Enabled: true, OnPass: false, OnFail: true}, auditSender),
	}

	runCollectionCycle(
		context.Background(),
		[]string{"/dev/sda", "/dev/sdb"},
		collector,
		evaluator,
		store,
		events,
		targets,
		testLogger(),
	)

	if len(events.published) != 2 {
		t.Fatalf("expected 2 published sample events, got %d", len(events.published))
	}
	if opsSender.calls != 1 {
		t.Fatalf("expected ops notification send count 1, got %d", opsSender.calls)
	}
	if auditSender.calls != 0 {
		t.Fatalf("expected audit notification send count 0, got %d", auditSender.calls)
	}

	if got := store.states[stateKey(1, "ops")]; got != string(health.StatusYellow) {
		t.Fatalf("expected sda ops state YELLOW, got %q", got)
	}
	if got := store.states[stateKey(1, "audit")]; got != string(health.StatusYellow) {
		t.Fatalf("expected sda audit state YELLOW, got %q", got)
	}
	if got := store.states[stateKey(2, "ops")]; got != string(health.StatusGreen) {
		t.Fatalf("expected sdb ops state GREEN, got %q", got)
	}
	if got := store.states[stateKey(2, "audit")]; got != string(health.StatusGreen) {
		t.Fatalf("expected sdb audit state GREEN, got %q", got)
	}
	if len(store.upserts) != 4 {
		t.Fatalf("expected 4 state upserts, got %d", len(store.upserts))
	}
}

func TestRunCollectionCycle_NotificationFailureDoesNotBlockOtherEntriesOrDrives(t *testing.T) {
	timestamp := time.Date(2026, 3, 19, 10, 5, 0, 0, time.UTC)
	collector := &fakeCollector{
		results: []smart.CollectResult{
			{Info: smart.DriveInfo{Device: "/dev/sda"}, Sample: smart.SmartSample{CollectedAt: timestamp, RawJSON: "/dev/sda"}},
			{Info: smart.DriveInfo{Device: "/dev/sdb"}, Sample: smart.SmartSample{CollectedAt: timestamp, RawJSON: "/dev/sdb"}},
		},
	}
	evaluator := &fakeEvaluator{
		byDevice: map[string]health.Result{
			"/dev/sda": {Status: health.StatusRed, Score: 20},
			"/dev/sdb": {Status: health.StatusRed, Score: 20},
		},
	}
	store := &fakeDaemonStore{
		driveIDByDevice: map[string]int64{
			"/dev/sda": 1,
			"/dev/sdb": 2,
		},
		states: map[string]string{
			stateKey(1, "failing"): string(health.StatusGreen),
			stateKey(1, "ok"):      string(health.StatusGreen),
			stateKey(2, "failing"): string(health.StatusGreen),
			stateKey(2, "ok"):      string(health.StatusGreen),
		},
	}
	events := &fakeEventPublisher{}

	failingSender := &fakeNotificationSender{err: errors.New("send failed")}
	okSender := &fakeNotificationSender{}
	targets := []notificationTarget{
		buildTestTarget(t, notification.Entry{Name: "failing", Enabled: true, OnPass: true, OnFail: true}, failingSender),
		buildTestTarget(t, notification.Entry{Name: "ok", Enabled: true, OnPass: true, OnFail: true}, okSender),
	}

	runCollectionCycle(
		context.Background(),
		[]string{"/dev/sda", "/dev/sdb"},
		collector,
		evaluator,
		store,
		events,
		targets,
		testLogger(),
	)

	if len(events.published) != 2 {
		t.Fatalf("expected 2 published sample events, got %d", len(events.published))
	}
	if failingSender.calls != 2 {
		t.Fatalf("expected failing sender called twice, got %d", failingSender.calls)
	}
	if okSender.calls != 2 {
		t.Fatalf("expected ok sender called twice, got %d", okSender.calls)
	}
	if len(store.upserts) != 2 {
		t.Fatalf("expected 2 dedupe state upserts, got %d", len(store.upserts))
	}
	for _, call := range store.upserts {
		if call.name != "ok" {
			t.Fatalf("expected failed notification states to skip persistence, got upsert %+v", call)
		}
		if call.state != string(health.StatusRed) {
			t.Fatalf("expected successful notification upsert to persist RED, got %+v", call)
		}
	}
	for _, key := range []string{stateKey(1, "failing"), stateKey(2, "failing")} {
		if store.states[key] != string(health.StatusGreen) {
			t.Fatalf("expected failed notification state %s to remain GREEN, got %q", key, store.states[key])
		}
	}
	for _, key := range []string{stateKey(1, "ok"), stateKey(2, "ok")} {
		if store.states[key] != string(health.StatusRed) {
			t.Fatalf("expected successful notification state %s to be RED, got %q", key, store.states[key])
		}
	}
}

func TestRunCollectionCycle_FailedSendRetriesSameNonGreenStatusNextCycle(t *testing.T) {
	timestamp := time.Date(2026, 3, 19, 10, 7, 0, 0, time.UTC)
	collector := &fakeCollector{
		results: []smart.CollectResult{
			{Info: smart.DriveInfo{Device: "/dev/sda"}, Sample: smart.SmartSample{CollectedAt: timestamp, RawJSON: "/dev/sda"}},
		},
	}
	evaluator := &fakeEvaluator{
		byDevice: map[string]health.Result{
			"/dev/sda": {Status: health.StatusRed, Score: 20},
		},
	}
	store := &fakeDaemonStore{
		driveIDByDevice: map[string]int64{"/dev/sda": 1},
		states:          map[string]string{stateKey(1, "ops"): string(health.StatusGreen)},
	}
	events := &fakeEventPublisher{}

	sender := &fakeNotificationSender{err: errors.New("send failed")}
	targets := []notificationTarget{
		buildTestTarget(t, notification.Entry{Name: "ops", Enabled: true, OnPass: true, OnFail: true}, sender),
	}

	runCollectionCycle(
		context.Background(),
		[]string{"/dev/sda"},
		collector,
		evaluator,
		store,
		events,
		targets,
		testLogger(),
	)

	if sender.calls != 1 {
		t.Fatalf("expected first cycle send attempt count 1, got %d", sender.calls)
	}
	if len(store.upserts) != 0 {
		t.Fatalf("expected failed first cycle to skip dedupe persistence, got %d upserts", len(store.upserts))
	}
	if got := store.states[stateKey(1, "ops")]; got != string(health.StatusGreen) {
		t.Fatalf("expected failed first cycle state to remain GREEN, got %q", got)
	}

	sender.err = nil
	collector.results[0].Sample.CollectedAt = timestamp.Add(time.Minute)

	runCollectionCycle(
		context.Background(),
		[]string{"/dev/sda"},
		collector,
		evaluator,
		store,
		events,
		targets,
		testLogger(),
	)

	if sender.calls != 2 {
		t.Fatalf("expected second cycle to retry same RED status, got %d total send attempts", sender.calls)
	}
	if len(store.upserts) != 1 {
		t.Fatalf("expected only successful retry to persist dedupe state, got %d upserts", len(store.upserts))
	}
	if got := store.upserts[0]; got.driveID != 1 || got.name != "ops" || got.state != string(health.StatusRed) {
		t.Fatalf("unexpected retry upsert: %+v", got)
	}
	if got := store.states[stateKey(1, "ops")]; got != string(health.StatusRed) {
		t.Fatalf("expected successful retry to persist RED state, got %q", got)
	}
}

func TestRunCollectionCycle_NewDriveCreatedOnInsertDispatchesNotificationSameCycle(t *testing.T) {
	timestamp := time.Date(2026, 3, 19, 10, 10, 0, 0, time.UTC)
	collector := &fakeCollector{
		results: []smart.CollectResult{
			{Info: smart.DriveInfo{Device: "/dev/sdz"}, Sample: smart.SmartSample{CollectedAt: timestamp, RawJSON: "/dev/sdz"}},
		},
	}
	evaluator := &fakeEvaluator{
		byDevice: map[string]health.Result{
			"/dev/sdz": {Status: health.StatusYellow, Score: 65},
		},
	}
	store := &fakeDaemonStore{
		driveIDByDevice: map[string]int64{},
		nextDriveID:     7,
		states:          map[string]string{},
	}
	events := &fakeEventPublisher{}

	sender := &fakeNotificationSender{}
	targets := []notificationTarget{
		buildTestTarget(t, notification.Entry{Name: "ops", Enabled: true, OnPass: false, OnFail: true}, sender),
	}

	runCollectionCycle(
		context.Background(),
		[]string{"/dev/sdz"},
		collector,
		evaluator,
		store,
		events,
		targets,
		testLogger(),
	)

	if len(events.published) != 1 {
		t.Fatalf("expected 1 published sample event, got %d", len(events.published))
	}
	if sender.calls != 1 {
		t.Fatalf("expected notification send count 1, got %d", sender.calls)
	}
	if store.listDrivesCalls != 2 {
		t.Fatalf("expected ListDrives to be called twice for refresh-on-miss, got %d", store.listDrivesCalls)
	}
	if driveID := store.driveIDByDevice["/dev/sdz"]; driveID != 7 {
		t.Fatalf("expected /dev/sdz to be assigned drive ID 7, got %d", driveID)
	}
	if len(store.upserts) != 1 {
		t.Fatalf("expected 1 dedupe state upsert, got %d", len(store.upserts))
	}
	if got := store.upserts[0]; got.driveID != 7 || got.name != "ops" || got.state != string(health.StatusYellow) {
		t.Fatalf("unexpected upsert: %+v", got)
	}
	if got := store.states[stateKey(7, "ops")]; got != string(health.StatusYellow) {
		t.Fatalf("expected /dev/sdz ops state YELLOW, got %q", got)
	}
}

type fakeCollector struct {
	results []smart.CollectResult
	err     error
}

func (f *fakeCollector) CollectAll(_ context.Context, _ []string) ([]smart.CollectResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

type fakeEvaluator struct {
	byDevice map[string]health.Result
}

func (f *fakeEvaluator) Evaluate(sample smart.SmartSample) health.Result {
	if out, ok := f.byDevice[sample.RawJSON]; ok {
		return out
	}
	return health.Result{Status: health.StatusGreen, Score: 95}
}

type fakeEventPublisher struct {
	published []string
}

func (f *fakeEventPublisher) Publish(eventType string, device string) {
	f.published = append(f.published, eventType+":"+device)
}

type upsertCall struct {
	driveID int64
	name    string
	state   string
}

type fakeDaemonStore struct {
	driveIDByDevice map[string]int64
	states          map[string]string
	upserts         []upsertCall
	nextDriveID     int64
	listDrivesCalls int
}

func (f *fakeDaemonStore) InsertSample(_ context.Context, info smart.DriveInfo, _ smart.SmartSample, _ health.Result) (int64, error) {
	if f.driveIDByDevice == nil {
		f.driveIDByDevice = map[string]int64{}
	}
	if _, ok := f.driveIDByDevice[info.Device]; !ok {
		nextDriveID := f.nextDriveID
		if nextDriveID == 0 {
			for _, driveID := range f.driveIDByDevice {
				if driveID >= nextDriveID {
					nextDriveID = driveID + 1
				}
			}
			if nextDriveID == 0 {
				nextDriveID = 1
			}
		}
		f.driveIDByDevice[info.Device] = nextDriveID
		f.nextDriveID = nextDriveID + 1
	}
	if _, ok := f.driveIDByDevice[info.Device]; !ok {
		return 0, fmt.Errorf("unknown device %s", info.Device)
	}
	return f.driveIDByDevice[info.Device], nil
}

func (f *fakeDaemonStore) ListDrives(_ context.Context) ([]storage.DriveSummary, error) {
	f.listDrivesCalls++
	out := make([]storage.DriveSummary, 0, len(f.driveIDByDevice))
	for device, id := range f.driveIDByDevice {
		out = append(out, storage.DriveSummary{ID: id, Device: device})
	}
	return out, nil
}

func (f *fakeDaemonStore) GetNotificationState(_ context.Context, driveID int64, notificationName string) (*storage.NotificationState, error) {
	state, ok := f.states[stateKey(driveID, notificationName)]
	if !ok {
		return nil, nil
	}
	return &storage.NotificationState{
		DriveID:          driveID,
		NotificationName: notificationName,
		State:            state,
		UpdatedAt:        time.Now().UTC(),
	}, nil
}

func (f *fakeDaemonStore) UpsertNotificationState(_ context.Context, driveID int64, notificationName string, state string, _ time.Time) error {
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[stateKey(driveID, notificationName)] = state
	f.upserts = append(f.upserts, upsertCall{
		driveID: driveID,
		name:    notificationName,
		state:   state,
	})
	return nil
}

type fakeNotificationFactory struct {
	senders map[string]*fakeNotificationSender
}

func (f fakeNotificationFactory) Build(entry notification.Entry) (notification.Sender, error) {
	sender, ok := f.senders[entry.Name]
	if !ok {
		return nil, fmt.Errorf("missing sender for %s", entry.Name)
	}
	return sender, nil
}

type fakeNotificationSender struct {
	calls int
	err   error
}

func (f *fakeNotificationSender) Send(_ context.Context, _ string, _ string) error {
	f.calls++
	return f.err
}

func buildTestTarget(t *testing.T, entry notification.Entry, sender *fakeNotificationSender) notificationTarget {
	t.Helper()
	dispatcher, err := notification.NewDispatcher(
		[]notification.Entry{entry},
		fakeNotificationFactory{senders: map[string]*fakeNotificationSender{entry.Name: sender}},
		time.Second,
	)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	return notificationTarget{name: entry.Name, dispatcher: dispatcher}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func stateKey(driveID int64, notificationName string) string {
	return fmt.Sprintf("%d:%s", driveID, notificationName)
}
