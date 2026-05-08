package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NyaMisty/dap-cli-go/internal/endpoint"
	"github.com/NyaMisty/dap-cli-go/internal/ipc"
	"github.com/NyaMisty/dap-cli-go/internal/model"
)

type ClientApp struct {
	root     string
	endpoint endpoint.Info
	conn     net.Conn
	decoder  *ipc.Decoder
	encoder  *ipc.Encoder
	clientID string
	running  atomic.Bool

	mu       sync.Cond
	messages []ipc.Envelope
	snapshot map[string]any
}

func NewClientApp(root string, ep endpoint.Info) (*ClientApp, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ep.Host, fmt.Sprint(ep.Port)), 2*time.Second)
	if err != nil {
		return nil, err
	}
	app := &ClientApp{
		root:     root,
		endpoint: ep,
		conn:     conn,
		decoder:  ipc.NewDecoder(conn),
		encoder:  ipc.NewEncoder(conn),
		clientID: model.NewID(),
		snapshot: map[string]any{},
	}
	app.running.Store(true)
	app.mu.L = &sync.Mutex{}
	go app.readerLoop()
	if err := app.hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := app.waitForSnapshot(func(snapshot map[string]any) bool { return len(snapshot) > 0 }, 3*time.Second); err != nil {
		app.running.Store(false)
		_ = conn.Close()
		return nil, err
	}
	return app, nil
}

func verifyEndpoint(ep endpoint.Info) bool {
	app, err := NewClientApp("", ep)
	if err != nil {
		return false
	}
	app.Close()
	return true
}

func EnsureDaemon(root string) (endpoint.Info, error) {
	if ep, err := endpoint.Read(root); err == nil && verifyEndpoint(ep) {
		return ep, nil
	}
	discovery, err := endpoint.Discover(root)
	if err != nil {
		return endpoint.Info{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return endpoint.Info{}, err
	}
	cmdArgs := []string{"--root", root, "--endpoint", discovery.Path}
	cmd := spawnDaemonCommand(exe, cmdArgs)
	cmd.Dir = root
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if os.Getenv("DAP_CLI_DAP_VERBOSE") == "1" {
		cmdArgs = append(cmdArgs, "--dap-verbose")
		cmd = spawnDaemonCommand(exe, cmdArgs)
		cmd.Dir = root
		cmd.Stdin = nil
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}
	if runtime.GOOS == "windows" {
		// Keep default process creation for portability; daemon stays alive independently of this client command.
	}
	if err := cmd.Start(); err != nil {
		return endpoint.Info{}, err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ep, err := endpoint.Read(root); err == nil && verifyEndpoint(ep) {
			return ep, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return endpoint.Info{}, fmt.Errorf("Failed to start dap daemon")
}

func spawnDaemonCommand(exe string, args []string) *exec.Cmd {
	if filepath.Base(exe) == "dap-daemon" || filepath.Base(exe) == "dap-daemon.exe" {
		return exec.Command(exe, args...)
	}
	return exec.Command(exe, append([]string{"daemon"}, args...)...)
}

func (a *ClientApp) Close() {
	a.running.Store(false)
	if a.conn != nil {
		_ = a.conn.Close()
	}
}

func (a *ClientApp) Request(requestType string, payload map[string]any) error {
	_, _, err := a.requestWait(requestType, payload, nil, 5*time.Second, 10*time.Second)
	return err
}

func (a *ClientApp) SnapshotCopy() map[string]any {
	a.mu.L.Lock()
	defer a.mu.L.Unlock()
	return cloneMap(a.snapshot)
}

func (a *ClientApp) RunCommand(command string, kwargs map[string]any) (map[string]any, error) {
	baseline := stringValue(a.snapshot["updated_at"])
	changed := func(snapshot map[string]any) bool { return stringValue(snapshot["updated_at"]) != baseline }
	switch command {
	case "status":
		_, snapshot, err := a.requestWait("daemon.status", nil, func(snapshot map[string]any) bool { return true }, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "attach":
		attach := mapValue(kwargs["attach"])
		predicate := func(snapshot map[string]any) bool {
			lifecycle := stringValue(snapshot["lifecycle"])
			if lifecycle == "running" || lifecycle == "stopped" || lifecycle == "terminated" || lifecycle == "exited" {
				return true
			}
			return attach["listen"] != nil && snapshot["debugpyWaitingForServer"] != nil
		}
		_, snapshot, err := a.requestWait("debug.attach", map[string]any{"attach": attach}, func(snapshot map[string]any) bool { return changed(snapshot) && predicate(snapshot) }, 5*time.Second, 20*time.Second)
		return snapshot, err
	case "break":
		path := stringValue(kwargs["path"])
		line := intValue(kwargs["line"])
		payload := map[string]any{"source": map[string]any{"path": path}, "breakpoints": []map[string]any{{"line": line}}}
		_, snapshot, err := a.requestWait("breakpoint.set", payload, func(snapshot map[string]any) bool {
			if !changed(snapshot) {
				return false
			}
			for _, bp := range sliceMapValue(snapshot["breakpoints"]) {
				candidate := intValue(bp["line"])
				if candidate == line {
					return true
				}
				if nested := mapValue(bp["breakpoint"]); intValue(nested["line"]) == line {
					return true
				}
			}
			return false
		}, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "clear-breaks":
		payload := map[string]any{"source": map[string]any{"path": stringValue(kwargs["path"])}}
		_, snapshot, err := a.requestWait("breakpoint.clear", payload, func(snapshot map[string]any) bool {
			return changed(snapshot) && len(sliceMapValue(snapshot["breakpoints"])) == 0
		}, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "continue":
		return a.waitLifecycleCommand("debug.continue", changed, []string{"running", "stopped", "terminated", "exited"}, 10*time.Second)
	case "pause":
		return a.waitLifecycleCommand("debug.pause", changed, []string{"stopped", "terminated", "exited"}, 20*time.Second)
	case "step":
		return a.waitLifecycleCommand("debug.step", changed, []string{"running", "stopped", "terminated", "exited"}, 10*time.Second)
	case "step-in":
		return a.waitLifecycleCommand("debug.step_in", changed, []string{"running", "stopped", "terminated", "exited"}, 10*time.Second)
	case "step-out":
		return a.waitLifecycleCommand("debug.step_out", changed, []string{"running", "stopped", "terminated", "exited"}, 10*time.Second)
	case "threads":
		_, snapshot, err := a.requestWait("debug.threads", nil, func(snapshot map[string]any) bool {
			return changed(snapshot) && (len(sliceMapValue(snapshot["threads"])) > 0 || terminal(snapshot))
		}, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "stack":
		_, snapshot, err := a.requestWait("debug.stack_trace", nil, func(snapshot map[string]any) bool {
			return changed(snapshot) && (mapValue(snapshot["last_stack_trace"])["completed"] == true || terminal(snapshot))
		}, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "scopes":
		_, snapshot, err := a.requestWait("debug.scopes", nil, func(snapshot map[string]any) bool {
			return changed(snapshot) && (len(sliceMapValue(snapshot["scopes"])) > 0 || terminal(snapshot))
		}, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "vars":
		ref := intValue(kwargs["variables_reference"])
		_, snapshot, err := a.requestWait("debug.variables", map[string]any{"variables_reference": ref}, func(snapshot map[string]any) bool { return changed(snapshot) }, 5*time.Second, 10*time.Second)
		return snapshot, err
	case "eval":
		payload := map[string]any{"expression": kwargs["expression"], "context": defaultString(stringValue(kwargs["context"]), "repl")}
		_, snapshot, err := a.requestWait("debug.evaluate", payload, func(snapshot map[string]any) bool {
			return changed(snapshot) && (mapValue(snapshot["last_evaluate"])["completed"] == true || terminal(snapshot))
		}, 5*time.Second, 20*time.Second)
		return snapshot, err
	case "stop":
		return a.waitLifecycleCommand("session.stop", changed, []string{"terminated"}, 10*time.Second)
	case "shutdown":
		return a.waitLifecycleCommand("daemon.shutdown", changed, []string{"terminated"}, 10*time.Second)
	default:
		return nil, fmt.Errorf("Unsupported command: %s", command)
	}
}

func (a *ClientApp) waitLifecycleCommand(requestType string, changed func(map[string]any) bool, lifecycles []string, snapshotTimeout time.Duration) (map[string]any, error) {
	allowed := map[string]bool{}
	for _, lifecycle := range lifecycles {
		allowed[lifecycle] = true
	}
	_, snapshot, err := a.requestWait(requestType, nil, func(snapshot map[string]any) bool {
		return changed(snapshot) && allowed[stringValue(snapshot["lifecycle"])]
	}, 5*time.Second, snapshotTimeout)
	return snapshot, err
}

func (a *ClientApp) hello() error {
	return a.send(ipc.NewEnvelope(ipc.KindHello, "client.hello", model.NewID(), "", map[string]any{"token": a.endpoint.Token, "client": map[string]any{"client_id": a.clientID, "name": "dap-cli", "cwd": mustCwd(), "pid": os.Getpid()}}))
}

func (a *ClientApp) requestWait(requestType string, payload map[string]any, predicate func(map[string]any) bool, responseTimeout, snapshotTimeout time.Duration) (ipc.Envelope, map[string]any, error) {
	requestID := model.NewID()
	body := map[string]any{"token": a.endpoint.Token}
	for k, v := range payload {
		body[k] = v
	}
	if err := a.send(ipc.NewEnvelope(ipc.KindRequest, requestType, requestID, "", body)); err != nil {
		return ipc.Envelope{}, nil, err
	}
	deadline := time.Now().Add(maxDuration(responseTimeout, snapshotTimeout))
	timer := time.AfterFunc(time.Until(deadline), func() {
		a.mu.L.Lock()
		a.mu.Broadcast()
		a.mu.L.Unlock()
	})
	defer timer.Stop()
	a.mu.L.Lock()
	defer a.mu.L.Unlock()
	lastSnapshot := cloneMap(a.snapshot)
	for {
		for _, message := range a.messages {
			if message.ID == requestID && (message.Kind == ipc.KindResponse || message.Kind == ipc.KindError) {
				if message.Kind == ipc.KindError {
					return message, lastSnapshot, fmt.Errorf("%v", message.Payload["error"])
				}
				if predicate == nil {
					return message, snapshotFromPayload(message.Payload, a.snapshot), nil
				}
			}
		}
		if len(a.snapshot) > 0 {
			lastSnapshot = cloneMap(a.snapshot)
			if predicate != nil && predicate(a.snapshot) {
				return ipc.Envelope{Kind: ipc.KindResponse, Type: requestType, ID: requestID}, lastSnapshot, nil
			}
		}
		if time.Now().After(deadline) {
			return ipc.Envelope{}, lastSnapshot, fmt.Errorf("Timed out waiting for %s", requestType)
		}
		a.mu.Wait()
	}
}

func (a *ClientApp) waitForSnapshot(predicate func(map[string]any) bool, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() {
		a.mu.L.Lock()
		a.mu.Broadcast()
		a.mu.L.Unlock()
	})
	defer timer.Stop()
	a.mu.L.Lock()
	defer a.mu.L.Unlock()
	for {
		if len(a.snapshot) > 0 && predicate(a.snapshot) {
			return cloneMap(a.snapshot), nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("Timed out waiting for snapshot")
		}
		a.mu.Wait()
	}
}

func (a *ClientApp) readerLoop() {
	for a.running.Load() {
		message, err := a.decoder.Decode()
		if err != nil {
			if err != io.EOF && a.running.Load() {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			break
		}
		a.mu.L.Lock()
		if snapshot := snapshotFromPayload(message.Payload, nil); snapshot != nil {
			a.snapshot = snapshot
		}
		a.messages = append(a.messages, message)
		if len(a.messages) > 500 {
			a.messages = a.messages[len(a.messages)-500:]
		}
		a.mu.Broadcast()
		a.mu.L.Unlock()
		debugPrint(message, a.snapshot)
	}
}

func (a *ClientApp) send(message ipc.Envelope) error { return a.encoder.Encode(message) }

func debugPrint(message ipc.Envelope, snapshot map[string]any) {
	if message.Kind == ipc.KindError {
		fmt.Fprintf(os.Stderr, "error: %v\n", message.Payload["error"])
		return
	}
	if message.Type == "daemon.hello" || message.Type == "session.state" || message.Type == "dap.output" {
		if len(snapshot) > 0 {
			fmt.Println(renderSnapshot(snapshot))
		}
	}
}

func snapshotFromPayload(payload map[string]any, fallback map[string]any) map[string]any {
	if snapshot, ok := payload["snapshot"]; ok {
		return mapValue(snapshot)
	}
	return fallback
}

func (a *ClientApp) LatestEvalResult() string {
	snapshot := a.SnapshotCopy()
	outputs := sliceMapValue(snapshot["recent_output"])
	for i := len(outputs) - 1; i >= 0; i-- {
		if stringValue(outputs[i]["category"]) == "eval" {
			return stringValue(outputs[i]["output"])
		}
	}
	return ""
}

func (a *ClientApp) LatestScopes() []map[string]any {
	snapshot := a.SnapshotCopy()
	return sliceMapValue(snapshot["scopes"])
}

func (a *ClientApp) LatestStackFrames() []map[string]any {
	snapshot := a.SnapshotCopy()
	return sliceMapValue(snapshot["stack_frames"])
}

func (a *ClientApp) LatestThreads() []map[string]any {
	snapshot := a.SnapshotCopy()
	return sliceMapValue(snapshot["threads"])
}

func (a *ClientApp) LatestVariables() []map[string]any {
	snapshot := a.SnapshotCopy()
	variables, _ := snapshot["variables"].(map[string][]map[string]any)
	if len(variables) == 0 {
		if raw := mapValue(snapshot["variables"]); len(raw) > 0 {
			var last string
			for key := range raw {
				last = key
			}
			return sliceMapValue(raw[last])
		}
		return nil
	}
	var last string
	for key := range variables {
		last = key
	}
	return variables[last]
}

func terminal(snapshot map[string]any) bool {
	lifecycle := stringValue(snapshot["lifecycle"])
	return lifecycle == "terminated" || lifecycle == "exited"
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for k, v := range input {
		output[k] = v
	}
	return output
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}
