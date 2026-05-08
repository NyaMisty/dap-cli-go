package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/NyaMisty/dap-cli-go/internal/model"
)

type Session struct {
	mu       sync.Mutex
	root     string
	snapshot model.SessionSnapshot
	clients  map[string]map[string]any
}

func NewSession(root string) *Session {
	return &Session{root: root, snapshot: model.NewSnapshot(root), clients: map[string]map[string]any{}}
}

func (s *Session) Snapshot() model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func (s *Session) SnapshotMap() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot.ToMap()
}

func (s *Session) AttachClient(client map[string]any) model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := stringValue(client["client_id"])
	if id == "" {
		id = model.NewID()
		client["client_id"] = id
	}
	s.clients[id] = client
	s.refreshClientsLocked()
	s.touchLocked()
	return s.snapshot
}

func (s *Session) DetachClient(clientID string) model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, clientID)
	s.refreshClientsLocked()
	s.touchLocked()
	return s.snapshot
}

func (s *Session) Reset() model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	clients := s.snapshot.Clients
	s.snapshot = model.NewSnapshot(s.root)
	s.snapshot.Clients = clients
	s.touchLocked()
	return s.snapshot
}

func (s *Session) SetLaunch(launch model.LaunchConfig) model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.AdapterID = launch.Adapter
	s.snapshot.AdapterCommand = launch.AdapterCommand()
	s.snapshot.Program = launch.Program
	s.snapshot.Args = launch.Args
	s.snapshot.CWD = launch.CWD
	s.snapshot.Extra["adapter"] = map[string]any{
		"adapter":      launch.Adapter,
		"request":      launch.Request,
		"command":      launch.Command,
		"adapter_args": launch.AdapterArgs,
		"args":         launch.Args,
		"program":      emptyStringNil(launch.Program),
		"cwd":          emptyStringNil(launch.CWD),
		"env":          launch.Env,
		"extra":        launch.Extra,
	}
	if attach, ok := launch.Extra["attach"]; ok {
		s.snapshot.Extra["attach"] = attach
	}
	s.touchLocked()
	return s.snapshot
}

func (s *Session) Update(changes map[string]any) model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyLocked(changes)
	s.touchLocked()
	return s.snapshot
}

func (s *Session) RecordEvent(event map[string]any) model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.RecentEvents = model.AppendLimited(s.snapshot.RecentEvents, event, model.HistoryLimit)
	s.touchLocked()
	return s.snapshot
}

func (s *Session) RecordOutput(output map[string]any) model.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.RecentOutput = model.AppendLimited(s.snapshot.RecentOutput, output, model.HistoryLimit)
	s.touchLocked()
	return s.snapshot
}

func (s *Session) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot.SessionID
}

func (s *Session) Root() string { return s.root }

func (s *Session) refreshClientsLocked() {
	clients := make([]map[string]any, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.snapshot.Clients = clients
}

func (s *Session) touchLocked() {
	s.snapshot.UpdatedAt = model.Now()
}

func (s *Session) applyLocked(changes map[string]any) {
	for key, value := range changes {
		switch key {
		case "lifecycle":
			s.snapshot.Lifecycle = stringValue(value)
		case "process_id":
			s.snapshot.ProcessID = value
		case "process_name":
			s.snapshot.ProcessName = stringValue(value)
		case "thread_id":
			s.snapshot.ThreadID = value
		case "frame_id":
			s.snapshot.FrameID = value
		case "source_path":
			s.snapshot.SourcePath = stringValue(value)
		case "line":
			s.snapshot.Line = value
		case "column":
			s.snapshot.Column = value
		case "stop_reason":
			s.snapshot.StopReason = stringValue(value)
		case "stop_description":
			s.snapshot.StopDescription = stringValue(value)
		case "breakpoints":
			s.snapshot.Breakpoints = sliceMapValue(value)
		case "threads":
			s.snapshot.Threads = sliceMapValue(value)
		case "stack_frames":
			s.snapshot.StackFrames = sliceMapValue(value)
		case "scopes":
			s.snapshot.Scopes = sliceMapValue(value)
		case "variables":
			s.snapshot.Variables = variablesValue(value)
		case "recent_output":
			s.snapshot.RecentOutput = sliceMapValue(value)
		case "extra":
			for k, v := range mapValue(value) {
				s.snapshot.Extra[k] = v
			}
		default:
			s.snapshot.Extra[key] = value
		}
	}
}

func defaultPython() string {
	if value := os.Getenv("DAP_CLI_PYTHON"); value != "" {
		return value
	}
	candidates := []string{"python"}
	if runtime.GOOS != "windows" {
		candidates = []string{"python3", "python"}
	}
	for _, candidate := range candidates {
		if path, err := lookPath(candidate); err == nil {
			return path
		}
	}
	return candidates[0]
}

var lookPath = func(file string) (string, error) {
	if filepath.IsAbs(file) {
		return file, nil
	}
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		path := filepath.Join(dir, file)
		if runtime.GOOS == "windows" {
			for _, ext := range []string{"", ".exe", ".bat", ".cmd"} {
				if isExecutable(path + ext) {
					return path + ext, nil
				}
			}
		} else if isExecutable(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", file)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func emptyStringNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
