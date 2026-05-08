package model

import "testing"

func TestAppendLimitedKeepsNewestItems(t *testing.T) {
	items := []map[string]any{}
	for i := 0; i < 55; i++ {
		items = AppendLimited(items, map[string]any{"i": i}, HistoryLimit)
	}
	if len(items) != HistoryLimit {
		t.Fatalf("len = %d, want %d", len(items), HistoryLimit)
	}
	if items[0]["i"] != 5 || items[len(items)-1]["i"] != 54 {
		t.Fatalf("unexpected retained range: first=%v last=%v", items[0], items[len(items)-1])
	}
}

func TestSnapshotToMapIncludesExtra(t *testing.T) {
	snapshot := NewSnapshot("/tmp/project")
	snapshot.Extra["debugpyWaitingForServer"] = map[string]any{"host": "127.0.0.1"}
	data := snapshot.ToMap()
	if data["root"] != "/tmp/project" || data["lifecycle"] != "idle" {
		t.Fatalf("unexpected base fields: %#v", data)
	}
	if data["debugpyWaitingForServer"] == nil {
		t.Fatalf("extra fields were not merged: %#v", data)
	}
}
