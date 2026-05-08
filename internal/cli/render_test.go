package cli

import (
	"strings"
	"testing"
)

func TestRenderSnapshotMatchesPythonShape(t *testing.T) {
	out := renderSnapshot(map[string]any{
		"session_id":  "s1",
		"adapter_id":  "debugpy",
		"lifecycle":   "stopped",
		"process_id":  42,
		"clients":     []any{map[string]any{"client_id": "c1"}},
		"program":     "target.py",
		"thread_id":   7,
		"frame_id":    8,
		"source_path": "target.py",
		"line":        9,
		"stop_reason": "breakpoint",
		"breakpoints": []any{map[string]any{"line": 9}},
	})
	for _, want := range []string{"Session s1 | debugpy | stopped | PID 42 | clients 1", "Target  target.py", "Focus   thread 7 | frame 8 | target.py:9 | stop breakpoint | breakpoints 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderStackPendingAndFrames(t *testing.T) {
	pending := renderStack(map[string]any{"last_stack_trace": map[string]any{"completed": false}})
	if pending != "Stack trace request is still pending." {
		t.Fatalf("pending = %q", pending)
	}
	frames := renderStack(map[string]any{"stack_frames": []any{map[string]any{"id": 1, "name": "main", "source": map[string]any{"path": "main.py"}, "line": 3}}})
	if frames != "#1 main main.py:3" {
		t.Fatalf("frames = %q", frames)
	}
}
