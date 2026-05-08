package cli

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestLatestHelpersExposeSnapshotData(t *testing.T) {
	app := &ClientApp{snapshot: map[string]any{
		"recent_output": []any{
			map[string]any{"category": "stdout", "output": "noop"},
			map[string]any{"category": "eval", "output": "2"},
		},
		"scopes": []any{map[string]any{"name": "Locals", "variablesReference": 3}},
		"stack_frames": []any{map[string]any{"id": 1, "name": "main"}},
		"threads": []any{map[string]any{"id": 7, "name": "MainThread"}},
		"variables": map[string]any{"9": []any{map[string]any{"name": "x", "value": "1"}}},
	}}
	app.mu.L = &sync.Mutex{}
	if got := app.LatestEvalResult(); got != "2" {
		t.Fatalf("LatestEvalResult = %q, want 2", got)
	}
	if scopes := app.LatestScopes(); len(scopes) != 1 || scopes[0]["name"] != "Locals" {
		t.Fatalf("LatestScopes = %#v", scopes)
	}
	if frames := app.LatestStackFrames(); len(frames) != 1 || frames[0]["name"] != "main" {
		t.Fatalf("LatestStackFrames = %#v", frames)
	}
	if threads := app.LatestThreads(); len(threads) != 1 || threads[0]["id"] != 7 {
		t.Fatalf("LatestThreads = %#v", threads)
	}
	if variables := app.LatestVariables(); len(variables) != 1 || variables[0]["name"] != "x" {
		t.Fatalf("LatestVariables = %#v", variables)
	}
}

func TestRunShellUnderstandsHelpAndQuit(t *testing.T) {
	app := &ClientApp{}
	app.mu.L = &sync.Mutex{}
	reader := strings.NewReader("help\nquit\n")
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	err := runShell(app, reader)
	_ = w.Close()
	outputBytes, _ := io.ReadAll(r)
	output := string(outputBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "dap>") || !strings.Contains(output, "attach connect <host> <port>") {
		t.Fatalf("unexpected shell output: %s", output)
	}
}
