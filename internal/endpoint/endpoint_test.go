package endpoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectKeyReplacesPathSeparators(t *testing.T) {
	key := ProjectKey(`C:\Users\Misty\repo/sub`)
	if strings.ContainsAny(key, `:\/`) {
		t.Fatalf("project key still contains path separators: %q", key)
	}
}

func TestFindProjectRootWalksUpToMarker(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "repo")
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindProjectRoot(child)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}
