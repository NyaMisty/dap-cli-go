package model

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

const HistoryLimit = 50

type LaunchConfig struct {
	Adapter     string            `json:"adapter" msgpack:"adapter"`
	Request     string            `json:"request" msgpack:"request"`
	Command     string            `json:"command" msgpack:"command"`
	AdapterArgs []string          `json:"adapter_args" msgpack:"adapter_args"`
	Args        []string          `json:"args" msgpack:"args"`
	Program     string            `json:"program,omitempty" msgpack:"program,omitempty"`
	CWD         string            `json:"cwd,omitempty" msgpack:"cwd,omitempty"`
	Env         map[string]string `json:"env" msgpack:"env"`
	Extra       map[string]any    `json:"extra" msgpack:"extra"`
}

func DefaultLaunchConfig(root, python string, attach map[string]any) LaunchConfig {
	if python == "" {
		python = "python"
	}
	return LaunchConfig{
		Adapter:     "debugpy",
		Request:     "attach",
		Command:     python,
		AdapterArgs: []string{"-m", "debugpy.adapter"},
		CWD:         root,
		Env:         map[string]string{},
		Extra:       map[string]any{"attach": attach},
	}
}

func (l LaunchConfig) AdapterCommand() string {
	cmd := l.Command
	for _, arg := range l.AdapterArgs {
		cmd += " " + arg
	}
	return cmd
}

type SessionSnapshot struct {
	SessionID       string                      `json:"session_id" msgpack:"session_id"`
	Root            string                      `json:"root" msgpack:"root"`
	AdapterID       string                      `json:"adapter_id,omitempty" msgpack:"adapter_id,omitempty"`
	AdapterCommand  string                      `json:"adapter_command,omitempty" msgpack:"adapter_command,omitempty"`
	Program         string                      `json:"program,omitempty" msgpack:"program,omitempty"`
	Args            []string                    `json:"args" msgpack:"args"`
	CWD             string                      `json:"cwd,omitempty" msgpack:"cwd,omitempty"`
	Lifecycle       string                      `json:"lifecycle" msgpack:"lifecycle"`
	ProcessID       any                         `json:"process_id,omitempty" msgpack:"process_id,omitempty"`
	ProcessName     string                      `json:"process_name,omitempty" msgpack:"process_name,omitempty"`
	ThreadID        any                         `json:"thread_id,omitempty" msgpack:"thread_id,omitempty"`
	FrameID         any                         `json:"frame_id,omitempty" msgpack:"frame_id,omitempty"`
	SourcePath      string                      `json:"source_path,omitempty" msgpack:"source_path,omitempty"`
	Line            any                         `json:"line,omitempty" msgpack:"line,omitempty"`
	Column          any                         `json:"column,omitempty" msgpack:"column,omitempty"`
	StopReason      string                      `json:"stop_reason,omitempty" msgpack:"stop_reason,omitempty"`
	StopDescription string                      `json:"stop_description,omitempty" msgpack:"stop_description,omitempty"`
	Clients         []map[string]any            `json:"clients" msgpack:"clients"`
	Breakpoints     []map[string]any            `json:"breakpoints" msgpack:"breakpoints"`
	Threads         []map[string]any            `json:"threads" msgpack:"threads"`
	StackFrames     []map[string]any            `json:"stack_frames" msgpack:"stack_frames"`
	Scopes          []map[string]any            `json:"scopes" msgpack:"scopes"`
	Variables       map[string][]map[string]any `json:"variables" msgpack:"variables"`
	RecentEvents    []map[string]any            `json:"recent_events" msgpack:"recent_events"`
	RecentOutput    []map[string]any            `json:"recent_output" msgpack:"recent_output"`
	UpdatedAt       string                      `json:"updated_at,omitempty" msgpack:"updated_at,omitempty"`
	Extra           map[string]any              `json:"-" msgpack:"-"`
}

func NewSnapshot(root string) SessionSnapshot {
	return SessionSnapshot{
		SessionID:    NewID(),
		Root:         root,
		Lifecycle:    "idle",
		Args:         []string{},
		Clients:      []map[string]any{},
		Breakpoints:  []map[string]any{},
		Threads:      []map[string]any{},
		StackFrames:  []map[string]any{},
		Scopes:       []map[string]any{},
		Variables:    map[string][]map[string]any{},
		RecentEvents: []map[string]any{},
		RecentOutput: []map[string]any{},
		UpdatedAt:    Now(),
		Extra:        map[string]any{},
	}
}

func (s SessionSnapshot) ToMap() map[string]any {
	data := map[string]any{
		"session_id":       s.SessionID,
		"root":             s.Root,
		"adapter_id":       emptyNil(s.AdapterID),
		"adapter_command":  emptyNil(s.AdapterCommand),
		"program":          emptyNil(s.Program),
		"args":             s.Args,
		"cwd":              emptyNil(s.CWD),
		"lifecycle":        s.Lifecycle,
		"process_id":       s.ProcessID,
		"process_name":     emptyNil(s.ProcessName),
		"thread_id":        s.ThreadID,
		"frame_id":         s.FrameID,
		"source_path":      emptyNil(s.SourcePath),
		"line":             s.Line,
		"column":           s.Column,
		"stop_reason":      emptyNil(s.StopReason),
		"stop_description": emptyNil(s.StopDescription),
		"clients":          s.Clients,
		"breakpoints":      s.Breakpoints,
		"threads":          s.Threads,
		"stack_frames":     s.StackFrames,
		"scopes":           s.Scopes,
		"variables":        s.Variables,
		"recent_events":    s.RecentEvents,
		"recent_output":    s.RecentOutput,
		"updated_at":       s.UpdatedAt,
	}
	for k, v := range s.Extra {
		data[k] = v
	}
	return data
}

func emptyNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func Now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buf)
}

func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func AppendLimited(items []map[string]any, item map[string]any, limit int) []map[string]any {
	items = append(items, item)
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}
