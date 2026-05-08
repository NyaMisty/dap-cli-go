package daemon

import "testing"

func TestKnownThreadIDFallsBackToSnapshotThreads(t *testing.T) {
	a, err := NewApp("/tmp/project", "/tmp/endpoint.json", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	a.session.Update(map[string]any{"threads": []map[string]any{{"id": 7, "name": "MainThread"}}})
	if got := a.knownThreadID(map[string]any{}); got != 7 {
		t.Fatalf("knownThreadID = %d, want 7", got)
	}
}

func TestApplyDAPResponseThreadsSetsCurrentThread(t *testing.T) {
	a, err := NewApp("/tmp/project", "/tmp/endpoint.json", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	a.applyDAPResponse(map[string]any{
		"command": "threads",
		"body": map[string]any{
			"threads": []any{map[string]any{"id": 3, "name": "MainThread"}},
		},
	})
	snapshot := a.session.Snapshot()
	if got := intValue(snapshot.ThreadID); got != 3 {
		t.Fatalf("thread_id = %d, want 3", got)
	}
	if len(snapshot.Threads) != 1 {
		t.Fatalf("threads len = %d, want 1", len(snapshot.Threads))
	}
}

func TestApplyStoppedStateFields(t *testing.T) {
	a, err := NewApp("/tmp/project", "/tmp/endpoint.json", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	a.session.Update(map[string]any{"lifecycle": "running", "thread_id": 1})
	a.session.Update(map[string]any{"lifecycle": "stopped", "thread_id": 5, "stop_reason": "breakpoint", "stop_description": "hit"})
	snapshot := a.session.Snapshot()
	if snapshot.Lifecycle != "stopped" {
		t.Fatalf("lifecycle = %q, want stopped", snapshot.Lifecycle)
	}
	if got := intValue(snapshot.ThreadID); got != 5 {
		t.Fatalf("thread_id = %d, want 5", got)
	}
	if snapshot.StopReason != "breakpoint" {
		t.Fatalf("stop_reason = %q", snapshot.StopReason)
	}
}

func TestInitializeResponseKeepsLifecycleInitializing(t *testing.T) {
	a, err := NewApp("/tmp/project", "/tmp/endpoint.json", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	a.session.Update(map[string]any{"lifecycle": "initializing"})
	a.applyDAPResponse(map[string]any{"command": "initialize", "success": true, "body": map[string]any{}})
	if got := a.session.Snapshot().Lifecycle; got != "initializing" {
		t.Fatalf("lifecycle = %q, want initializing", got)
	}
}
