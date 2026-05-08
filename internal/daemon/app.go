package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/NyaMisty/dap-cli-go/internal/dapclient"
	"github.com/NyaMisty/dap-cli-go/internal/endpoint"
	"github.com/NyaMisty/dap-cli-go/internal/ipc"
	"github.com/NyaMisty/dap-cli-go/internal/model"
	godap "github.com/google/go-dap"
)

type App struct {
	root         string
	endpointPath string
	token        string
	logger       *slog.Logger
	session      *Session
	dapVerbose   bool

	server  net.Listener
	clients map[*ClientHandler]struct{}
	mu      sync.Mutex

	dapMu             sync.Mutex
	dap               *dapclient.Client
	adapterCmd        *exec.Cmd
	adapterConn       net.Conn
	pendingAttach     map[string]any
	startupPhase      string
	pauseAfterThreads bool
	shuttingDown      bool
	adapterWaitDone   chan struct{}
}

func NewApp(root, endpointPath string, logger *slog.Logger, dapVerbose bool) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	token, err := model.NewToken()
	if err != nil {
		return nil, err
	}
	return &App{
		root:         root,
		endpointPath: endpointPath,
		token:        token,
		logger:       logger,
		session:      NewSession(root),
		dapVerbose:   dapVerbose,
		clients:      map[*ClientHandler]struct{}{},
	}, nil
}

func Serve(root, endpointPath, host string, port int, logger *slog.Logger, dapVerbose bool) error {
	app, err := NewApp(root, endpointPath, logger, dapVerbose)
	if err != nil {
		return err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return err
	}
	app.server = listener
	addr := listener.Addr().(*net.TCPAddr)
	info := endpoint.Info{Path: endpointPath, Host: host, Port: addr.Port, Token: app.token, PID: os.Getpid()}
	if err := endpoint.Write(root, info); err != nil {
		_ = listener.Close()
		return err
	}
	logger.Info("daemon listening", "host", host, "port", addr.Port, "endpoint", endpointPath)
	defer func() {
		_ = endpoint.Remove(root)
		app.StopSession()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if app.shuttingDown || errors.Is(err, net.ErrClosed) {
				return nil
			}
			logger.Warn("accept failed", "error", err)
			continue
		}
		handler := NewClientHandler(app, conn)
		app.register(handler)
		go handler.Run()
	}
}

func (a *App) register(handler *ClientHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clients[handler] = struct{}{}
}

func (a *App) unregister(handler *ClientHandler) {
	a.mu.Lock()
	delete(a.clients, handler)
	a.mu.Unlock()
	if handler.clientID != "" {
		a.session.DetachClient(handler.clientID)
		a.Broadcast("clients.changed", map[string]any{"snapshot": a.session.SnapshotMap()})
	}
}

func (a *App) Handle(handler *ClientHandler, msg ipc.Envelope) ipc.Envelope {
	if msg.V != ipc.ProtocolVersion {
		return ipc.ErrorEnvelope(msg, fmt.Sprintf("Unsupported protocol version: %v", msg.V))
	}
	if stringValue(msg.Payload["token"]) != a.token {
		return ipc.ErrorEnvelope(msg, "Authentication failed")
	}
	if msg.Kind == ipc.KindHello && msg.Type == "client.hello" {
		client := mapValue(msg.Payload["client"])
		snapshot := a.session.AttachClient(client).ToMap()
		handler.clientID = stringValue(client["client_id"])
		return ipc.NewEnvelope(ipc.KindHello, "daemon.hello", msg.ID, a.session.SessionID(), map[string]any{
			"daemon_id":        fmt.Sprint(os.Getpid()),
			"protocol_version": ipc.ProtocolVersion,
			"snapshot":         snapshot,
		})
	}
	if msg.Kind != ipc.KindRequest {
		return ipc.ErrorEnvelope(msg, fmt.Sprintf("Unsupported message kind: %s", msg.Kind))
	}
	payload, err := a.handleRequest(msg.Type, msg.Payload)
	if err != nil {
		a.logger.Warn("request failed", "type", msg.Type, "error", err)
		return ipc.ErrorEnvelope(msg, err.Error())
	}
	return ipc.NewEnvelope(ipc.KindResponse, msg.Type, msg.ID, a.session.SessionID(), payload)
}

func (a *App) handleRequest(requestType string, payload map[string]any) (map[string]any, error) {
	switch requestType {
	case "daemon.status", "session.attach", "session.state":
		return map[string]any{"snapshot": a.session.SnapshotMap()}, nil
	case "debug.attach":
		if err := a.StartAttach(mapValue(payload["attach"])); err != nil {
			return nil, err
		}
	case "breakpoint.set":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		source := mapValue(payload["source"])
		if len(source) == 0 && payload["path"] != nil {
			source = map[string]any{"path": payload["path"]}
		}
		breakpoints := sliceMapValue(payload["breakpoints"])
		if len(breakpoints) == 0 {
			if line := intValue(payload["line"]); line > 0 {
				breakpoints = []map[string]any{{"line": line}}
			}
			for _, raw := range sliceAnyValue(payload["lines"]) {
				breakpoints = append(breakpoints, map[string]any{"line": intValue(raw)})
			}
		}
		if err := a.sendDAP("setBreakpoints", func() error { return a.dap.SetBreakpoints(source, breakpoints) }); err != nil {
			return nil, err
		}
		stored := make([]map[string]any, 0, len(breakpoints))
		for _, bp := range breakpoints {
			item := map[string]any{"source": source}
			for k, v := range bp {
				item[k] = v
			}
			stored = append(stored, item)
		}
		a.session.Update(map[string]any{"breakpoints": stored})
		a.BroadcastState()
	case "breakpoint.clear":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		source := mapValue(payload["source"])
		if len(source) == 0 && payload["path"] != nil {
			source = map[string]any{"path": payload["path"]}
		}
		if err := a.sendDAP("setBreakpoints", func() error { return a.dap.SetBreakpoints(source, nil) }); err != nil {
			return nil, err
		}
		a.session.Update(map[string]any{"breakpoints": []map[string]any{}})
		a.BroadcastState()
	case "breakpoint.list":
		snapshot := a.session.Snapshot()
		return map[string]any{"breakpoints": snapshot.Breakpoints, "snapshot": snapshot.ToMap()}, nil
	case "debug.continue":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		threadID := a.knownThreadID(payload)
		if threadID == 0 {
			return nil, fmt.Errorf("No thread selected")
		}
		if err := a.sendDAP("continue", func() error { return a.dap.Continue(threadID) }); err != nil {
			return nil, err
		}
	case "debug.pause":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		threadID := a.knownThreadID(payload)
		if threadID == 0 {
			a.pauseAfterThreads = true
			if err := a.sendDAP("threads", a.dap.Threads); err != nil {
				return nil, err
			}
		} else if err := a.sendDAP("pause", func() error { return a.dap.Pause(threadID) }); err != nil {
			return nil, err
		}
	case "debug.step", "debug.step_over":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		threadID := a.knownThreadID(payload)
		if threadID == 0 {
			return nil, fmt.Errorf("No thread selected")
		}
		if err := a.sendDAP("next", func() error { return a.dap.Next(threadID) }); err != nil {
			return nil, err
		}
	case "debug.step_in":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		threadID := a.knownThreadID(payload)
		if threadID == 0 {
			return nil, fmt.Errorf("No thread selected")
		}
		if err := a.sendDAP("stepIn", func() error { return a.dap.StepIn(threadID) }); err != nil {
			return nil, err
		}
	case "debug.step_out":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		threadID := a.knownThreadID(payload)
		if threadID == 0 {
			return nil, fmt.Errorf("No thread selected")
		}
		if err := a.sendDAP("stepOut", func() error { return a.dap.StepOut(threadID) }); err != nil {
			return nil, err
		}
	case "debug.threads":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		if err := a.sendDAP("threads", a.dap.Threads); err != nil {
			return nil, err
		}
	case "debug.stack_trace":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		threadID := a.knownThreadID(payload)
		if threadID == 0 {
			return nil, fmt.Errorf("No thread selected")
		}
		startFrame := intValue(payload["startFrame"])
		levels := intValue(payload["levels"])
		if levels == 0 {
			levels = 20
		}
		extra := a.session.Snapshot().Extra
		extra["last_stack_trace"] = map[string]any{"completed": false, "success": nil, "message": nil, "totalFrames": nil, "frameCount": nil}
		a.session.Update(map[string]any{"extra": extra})
		if err := a.sendDAP("stackTrace", func() error { return a.dap.StackTrace(threadID, startFrame, levels) }); err != nil {
			return nil, err
		}
	case "debug.scopes":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		frameID := intValue(payload["frame_id"])
		if frameID == 0 {
			frameID = intValue(a.session.Snapshot().FrameID)
		}
		if frameID <= 0 {
			return nil, fmt.Errorf("No frame selected")
		}
		if err := a.sendDAP("scopes", func() error { return a.dap.Scopes(frameID) }); err != nil {
			return nil, err
		}
	case "debug.variables":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		ref := intValue(payload["variables_reference"])
		if ref == 0 {
			ref = intValue(payload["ref"])
		}
		if ref <= 0 {
			return nil, fmt.Errorf("variables_reference is required")
		}
		if err := a.sendDAP("variables", func() error { return a.dap.Variables(ref) }); err != nil {
			return nil, err
		}
	case "debug.evaluate":
		if err := a.ensureActiveAdapter(); err != nil {
			return nil, err
		}
		expression := stringValue(payload["expression"])
		if expression == "" {
			return nil, fmt.Errorf("expression is required")
		}
		context := stringValue(payload["context"])
		if context == "" {
			context = "repl"
		}
		frameID := intValue(payload["frame_id"])
		extra := a.session.Snapshot().Extra
		extra["last_evaluate"] = map[string]any{"completed": false, "expression": expression, "context": context, "frame_id": frameID, "success": nil, "message": nil, "result": nil}
		a.session.Update(map[string]any{"extra": extra})
		if err := a.sendDAP("evaluate", func() error { return a.dap.Evaluate(expression, frameID, context) }); err != nil {
			return nil, err
		}
	case "session.stop":
		a.StopSession()
	case "daemon.shutdown":
		a.Shutdown()
	default:
		return nil, fmt.Errorf("Unsupported request: %s", requestType)
	}
	return map[string]any{"snapshot": a.session.SnapshotMap()}, nil
}

func (a *App) logDAP(direction string, message map[string]any) {
	if !a.dapVerbose {
		return
	}
	data, err := json.Marshal(message)
	if err != nil {
		a.logger.Debug("dap packet", "direction", direction, "message", message)
		return
	}
	fmt.Fprintf(os.Stderr, "DAP %s %s\n", direction, data)
}

func (a *App) sendDAP(command string, fn func() error) error {
	a.logDAP("tx", map[string]any{"type": "request", "command": command})
	return fn()
}

func (a *App) sendInitialize() error {
	return a.sendDAP("initialize", a.dap.Initialize)
}

func (a *App) StartAttach(attach map[string]any) error {
	connectInfo := mapValue(attach["connect"])
	listenInfo := mapValue(attach["listen"])
	if len(connectInfo) > 0 && len(listenInfo) > 0 {
		return fmt.Errorf("attach.connect and attach.listen are mutually exclusive")
	}
	if len(connectInfo) == 0 && len(listenInfo) == 0 {
		return fmt.Errorf("attach requires connect or listen")
	}
	python := stringValue(attach["command"])
	if python == "" {
		python = defaultPython()
	}
	launch := model.DefaultLaunchConfig(a.root, python, attach)
	if args := sliceStringValue(attach["adapter_args"]); len(args) > 0 {
		launch.AdapterArgs = args
	}
	if adapter := stringValue(attach["adapter"]); adapter != "" {
		launch.Adapter = adapter
	}
	a.dapMu.Lock()
	defer a.dapMu.Unlock()
	a.resetForNewSession()
	a.session.SetLaunch(launch)
	a.session.Update(map[string]any{"lifecycle": "initializing"})
	a.pendingAttach = attach
	if len(connectInfo) > 0 {
		host := stringValue(connectInfo["host"])
		if host == "" {
			host = "localhost"
		}
		port := intValue(connectInfo["port"])
		if port == 0 {
			return fmt.Errorf("attach.connect.port is required")
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), 5*time.Second)
		if err != nil {
			return err
		}
		a.adapterConn = conn
		a.dap = dapclient.New(conn, conn)
		go a.dapReader(a.session.SessionID())
	} else {
		if intValue(listenInfo["port"]) == 0 {
			return fmt.Errorf("attach.listen.port is required")
		}
		if err := a.ensureAdapterProcess(launch); err != nil {
			return err
		}
	}
	if err := a.sendInitialize(); err != nil {
		return err
	}
	if len(connectInfo) > 0 {
		a.startupPhase = "attach_wait_initialized"
	}
	a.BroadcastState()
	return nil
}

func (a *App) ensureAdapterProcess(launch model.LaunchConfig) error {
	if a.adapterCmd != nil && a.adapterCmd.Process != nil && a.adapterCmd.ProcessState == nil {
		return nil
	}
	cmd := exec.Command(launch.Command, launch.AdapterArgs...)
	cmd.Dir = launch.CWD
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	a.adapterCmd = cmd
	a.adapterWaitDone = make(chan struct{})
	a.dap = dapclient.New(stdout, stdin)
	sessionID := a.session.SessionID()
	go a.dapReader(sessionID)
	go a.stderrReader(sessionID, stderr)
	go func() {
		_ = cmd.Wait()
		close(a.adapterWaitDone)
		a.dapMu.Lock()
		defer a.dapMu.Unlock()
		if sessionID == a.session.SessionID() && a.adapterCmd == cmd {
			a.finalizeAdapterExit("exited")
		}
	}()
	return nil
}

func (a *App) dapReader(sessionID string) {
	for {
		event, err := a.dap.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				a.logger.Debug("dap reader stopped", "error", err)
			}
			break
		}
		a.dapMu.Lock()
		a.logDAP("rx", event.Raw)
		if sessionID != a.session.SessionID() {
			a.dapMu.Unlock()
			return
		}
		a.applyDAPEvent(event)
		a.dapMu.Unlock()
	}
	a.dapMu.Lock()
	defer a.dapMu.Unlock()
	if sessionID == a.session.SessionID() {
		a.finalizeAdapterExit("exited")
	}
}

func (a *App) stderrReader(sessionID string, stderr io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := stderr.Read(buf)
		if n > 0 {
			if sessionID != a.session.SessionID() {
				return
			}
			text := string(buf[:n])
			output := map[string]any{"category": "stderr", "output": text}
			a.session.RecordOutput(output)
			a.Broadcast("dap.output", map[string]any{"output": output, "snapshot": a.session.SnapshotMap()})
		}
		if err != nil {
			return
		}
	}
}

func (a *App) applyDAPEvent(event dapclient.WireEvent) {
	body := event.Body
	a.session.RecordEvent(map[string]any{"event": event.Event, "body": body, "type": event.Type})
	switch event.Event {
	case "initialized":
		if a.startupPhase == "attach_wait_initialized" {
			a.sendPostInitializedRequests()
			a.startupPhase = ""
		} else {
			return
		}
	case "response":
		a.logger.Debug("dap response", "command", body["command"], "success", body["success"], "message", body["message"])
		if stringValue(body["command"]) == "initialize" && a.pendingAttach != nil {
			attach := a.pendingAttach
			a.pendingAttach = nil
			if err := a.sendDAP("attach", func() error { return a.dap.Attach(a.attachArguments(attach)) }); err != nil {
				a.logger.Warn("attach request failed", "error", err)
			}
			a.startupPhase = "attach_wait_initialized"
		} else {
			a.applyDAPResponse(body)
		}
	case "thread":
		threadID := body["threadId"]
		if stringValue(body["reason"]) == "started" && intValue(threadID) != 0 {
			threads := a.session.Snapshot().Threads
			filtered := make([]map[string]any, 0, len(threads)+1)
			for _, thread := range threads {
				if intValue(thread["id"]) != intValue(threadID) {
					filtered = append(filtered, thread)
				}
			}
			filtered = append(filtered, map[string]any{"id": threadID, "name": fmt.Sprintf("Thread-%v", threadID)})
			changes := map[string]any{"threads": filtered}
			if intValue(a.session.Snapshot().ThreadID) == 0 {
				changes["thread_id"] = threadID
			}
			a.session.Update(changes)
		}
	case "debugpyWaitingForServer", "debugpySockets":
		a.session.Update(map[string]any{event.Event: body})
	case "output":
		output := map[string]any{"category": defaultString(stringValue(body["category"]), "stdout"), "output": stringValue(body["output"]), "body": body}
		a.session.RecordOutput(output)
	case "process":
		a.session.Update(map[string]any{"process_id": body["systemProcessId"], "process_name": body["name"]})
		if err := a.sendDAP("threads", a.dap.Threads); err != nil {
			a.logger.Warn("threads failed", "error", err)
		}
	case "stopped":
		threadID := body["threadId"]
		a.session.Update(map[string]any{"lifecycle": "stopped", "thread_id": threadID, "stop_reason": body["reason"], "stop_description": body["description"]})
		if err := a.sendDAP("threads", a.dap.Threads); err != nil {
			a.logger.Warn("threads failed", "error", err)
		}
		if id := intValue(threadID); id > 0 {
			if err := a.sendDAP("stackTrace", func() error { return a.dap.StackTrace(id, 0, 1) }); err != nil {
				a.logger.Warn("stackTrace failed", "error", err)
			}
			if err := a.sendDAP("stackTrace", func() error { return a.dap.StackTrace(id, 0, 19) }); err != nil {
				a.logger.Warn("stackTrace failed", "error", err)
			}
		}
	case "continued":
		a.session.Update(map[string]any{"lifecycle": "running", "stop_reason": "", "stop_description": ""})
	case "exited":
		a.session.Update(map[string]any{"lifecycle": "exited"})
	case "terminated":
		a.session.Update(map[string]any{"lifecycle": "terminated"})
	default:
		return
	}
	a.Broadcast("dap."+event.Event, map[string]any{"event": map[string]any{"event": event.Event, "body": body, "type": event.Type}, "snapshot": a.session.SnapshotMap()})
	a.BroadcastState()
}

func (a *App) applyDAPResponse(body map[string]any) {
	command := stringValue(body["command"])
	responseBody := mapValue(body["body"])
	switch command {
	case "threads":
		threads := sliceMapValue(responseBody["threads"])
		changes := map[string]any{"threads": threads}
		if len(threads) > 0 && intValue(a.session.Snapshot().ThreadID) == 0 {
			changes["thread_id"] = threads[0]["id"]
		}
		a.session.Update(changes)
		if a.pauseAfterThreads && len(threads) > 0 {
			a.pauseAfterThreads = false
			if err := a.sendDAP("pause", func() error { return a.dap.Pause(intValue(threads[0]["id"])) }); err != nil {
				a.logger.Warn("pause failed", "error", err)
			}
		}
	case "stackTrace":
		frames := sliceMapValue(responseBody["stackFrames"])
		extra := a.session.Snapshot().Extra
		extra["last_stack_trace"] = map[string]any{"completed": true, "success": successValue(body), "message": body["message"], "totalFrames": responseBody["totalFrames"], "frameCount": len(frames), "dap_request_seq": body["request_seq"]}
		changes := map[string]any{"stack_frames": frames, "extra": extra}
		if len(frames) > 0 {
			frame := frames[0]
			source := mapValue(frame["source"])
			changes["frame_id"] = frame["id"]
			changes["source_path"] = firstString(source["path"], source["name"])
			changes["line"] = frame["line"]
			changes["column"] = frame["column"]
		}
		a.session.Update(changes)
	case "scopes":
		a.session.Update(map[string]any{"scopes": sliceMapValue(responseBody["scopes"])})
	case "variables":
		variables := a.session.Snapshot().Variables
		key := fmt.Sprint(body["request_seq"])
		if key == "<nil>" || key == "" {
			key = fmt.Sprint(len(variables))
		}
		variables[key] = sliceMapValue(responseBody["variables"])
		a.session.Update(map[string]any{"variables": variables})
	case "setBreakpoints":
		breakpoints := sliceMapValue(responseBody["breakpoints"])
		if len(breakpoints) == 0 {
			breakpoints = a.session.Snapshot().Breakpoints
		}
		a.session.Update(map[string]any{"breakpoints": breakpoints})
	case "evaluate":
		extra := a.session.Snapshot().Extra
		previous := mapValue(extra["last_evaluate"])
		for k, v := range map[string]any{"completed": true, "success": successValue(body), "message": body["message"], "result": responseBody["result"], "dap_request_seq": body["request_seq"]} {
			previous[k] = v
		}
		extra["last_evaluate"] = previous
		outputs := a.session.Snapshot().RecentOutput
		filtered := make([]map[string]any, 0, len(outputs)+1)
		for _, output := range outputs {
			if output["category"] != "eval" {
				filtered = append(filtered, output)
			}
		}
		filtered = append(filtered, map[string]any{"category": "eval", "output": responseBody["result"], "body": responseBody})
		if len(filtered) > model.HistoryLimit {
			filtered = filtered[len(filtered)-model.HistoryLimit:]
		}
		a.session.Update(map[string]any{"recent_output": filtered, "extra": extra})
	case "attach", "configurationDone":
		if a.session.Snapshot().Lifecycle == "initializing" {
			a.session.Update(map[string]any{"lifecycle": "running"})
		}
	case "disconnect":
		a.session.Update(map[string]any{"lifecycle": "terminated"})
	case "continue", "next", "stepIn", "stepOut":
		a.session.Update(map[string]any{"lifecycle": "running"})
	}
}

func (a *App) sendPostInitializedRequests() {
	if err := a.sendDAP("setFunctionBreakpoints", func() error { return a.dap.SetFunctionBreakpoints([]godap.FunctionBreakpoint{}) }); err != nil {
		a.logger.Warn("setFunctionBreakpoints failed", "error", err)
	}
	if err := a.sendDAP("setExceptionBreakpoints", func() error { return a.dap.SetExceptionBreakpoints([]string{"uncaught"}) }); err != nil {
		a.logger.Warn("setExceptionBreakpoints failed", "error", err)
	}
	if err := a.sendDAP("configurationDone", a.dap.ConfigurationDone); err != nil {
		a.logger.Warn("configurationDone failed", "error", err)
	}
	if err := a.sendDAP("threads", a.dap.Threads); err != nil {
		a.logger.Warn("threads failed", "error", err)
	}
	a.session.Update(map[string]any{"lifecycle": "running"})
}

func (a *App) attachArguments(attach map[string]any) map[string]any {
	args := map[string]any{
		"name":                  defaultString(stringValue(attach["name"]), "Python Debugger: Remote Attach"),
		"type":                  defaultString(stringValue(attach["type"]), "debugpy"),
		"request":               defaultString(stringValue(attach["request"]), "attach"),
		"pathMappings":          []map[string]any{{"localRoot": a.root, "remoteRoot": "."}},
		"__configurationTarget": 6,
		"clientOS":              clientOS(),
		"debugOptions":          []string{"RedirectOutput", "ShowReturnValue"},
		"justMyCode":            true,
		"showReturnValue":       true,
		"workspaceFolder":       a.root,
		"__sessionId":           a.session.SessionID(),
	}
	for _, key := range []string{"pathMappings", "__configurationTarget", "clientOS", "debugOptions", "justMyCode", "showReturnValue", "workspaceFolder", "__sessionId"} {
		if value, ok := attach[key]; ok {
			args[key] = value
		}
	}
	if a.adapterConn == nil && attach["listen"] != nil {
		args["listen"] = mapValue(attach["listen"])
	}
	return args
}

func (a *App) ensureActiveAdapter() error {
	if a.dap != nil {
		return nil
	}
	return fmt.Errorf("No active debug session")
}

func (a *App) terminateAdapterProcess() {
	cmd := a.adapterCmd
	waitDone := a.adapterWaitDone
	a.adapterCmd = nil
	a.adapterWaitDone = nil
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState == nil {
		_ = cmd.Process.Kill()
	}
	if waitDone != nil {
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *App) StopSession() {
	a.dapMu.Lock()
	defer a.dapMu.Unlock()
	if a.dap != nil && a.adapterConn == nil {
		_ = a.sendDAP("disconnect", a.dap.Disconnect)
	}
	if a.adapterConn != nil {
		_ = a.adapterConn.Close()
		a.adapterConn = nil
	}
	a.terminateAdapterProcess()
	a.finalizeAdapterExit("terminated")
}

func (a *App) Shutdown() {
	a.StopSession()
	a.shuttingDown = true
	if a.server != nil {
		_ = a.server.Close()
	}
}

func (a *App) resetForNewSession() {
	a.pendingAttach = nil
	a.startupPhase = ""
	a.pauseAfterThreads = false
	if a.adapterConn != nil {
		_ = a.adapterConn.Close()
		a.adapterConn = nil
	}
	a.terminateAdapterProcess()
	a.dap = nil
	a.session.Reset()
}

func (a *App) finalizeAdapterExit(lifecycle string) {
	a.dap = nil
	a.adapterCmd = nil
	a.adapterWaitDone = nil
	a.adapterConn = nil
	a.pendingAttach = nil
	a.startupPhase = ""
	a.pauseAfterThreads = false
	a.session.Update(map[string]any{"lifecycle": lifecycle})
	a.BroadcastState()
}

func (a *App) knownThreadID(payload map[string]any) int {
	if id := intValue(payload["thread_id"]); id > 0 {
		return id
	}
	if id := intValue(payload["threadId"]); id > 0 {
		return id
	}
	if id := intValue(a.session.Snapshot().ThreadID); id > 0 {
		return id
	}
	threads := a.session.Snapshot().Threads
	if len(threads) > 0 {
		return intValue(threads[0]["id"])
	}
	return 0
}

func (a *App) BroadcastState() {
	a.Broadcast("session.state", map[string]any{"snapshot": a.session.SnapshotMap()})
}

func (a *App) Broadcast(eventType string, payload map[string]any) {
	msg := ipc.NewEnvelope(ipc.KindEvent, eventType, "", a.session.SessionID(), payload)
	a.mu.Lock()
	clients := make([]*ClientHandler, 0, len(a.clients))
	for c := range a.clients {
		clients = append(clients, c)
	}
	a.mu.Unlock()
	for _, client := range clients {
		if err := client.Send(msg); err != nil {
			a.unregister(client)
		}
	}
}

func sliceAnyValue(value any) []any {
	if v, ok := value.([]any); ok {
		return v
	}
	return nil
}

func sliceStringValue(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, stringValue(item))
	}
	return out
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func firstString(values ...any) string {
	for _, value := range values {
		if s := stringValue(value); s != "" {
			return s
		}
	}
	return ""
}

func successValue(body map[string]any) bool {
	if value, ok := body["success"].(bool); ok {
		return value
	}
	return true
}

func clientOS() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "linux"
}
