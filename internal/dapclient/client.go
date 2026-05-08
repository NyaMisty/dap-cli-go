package dapclient

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	godap "github.com/google/go-dap"
)

type WireEvent struct {
	Event string         `json:"event"`
	Body  map[string]any `json:"body"`
	Type  string         `json:"type"`
	Raw   map[string]any `json:"raw"`
}

type Client struct {
	mu     sync.Mutex
	seq    int
	reader *bufio.Reader
	writer io.Writer
}

func New(reader io.Reader, writer io.Writer) *Client {
	return &Client{reader: bufio.NewReader(reader), writer: writer}
}

func (c *Client) Read() (WireEvent, error) {
	data, err := godap.ReadBaseMessage(c.reader)
	if err != nil {
		return WireEvent{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return WireEvent{}, err
	}
	messageType := stringValue(raw["type"])
	body := mapValue(raw["body"])
	if messageType == "response" {
		return WireEvent{Event: "response", Type: messageType, Body: raw, Raw: raw}, nil
	}
	if messageType == "event" {
		return WireEvent{Event: stringValue(raw["event"]), Type: messageType, Body: body, Raw: raw}, nil
	}
	if messageType == "request" {
		return WireEvent{Event: "request", Type: messageType, Body: raw, Raw: raw}, nil
	}
	return WireEvent{}, fmt.Errorf("unsupported DAP message type %q", messageType)
}

func (c *Client) Send(message godap.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if setter, ok := message.(interface{ GetRequest() *godap.Request }); ok {
		req := setter.GetRequest()
		c.seq++
		req.Seq = c.seq
		req.Type = "request"
	}
	return godap.WriteProtocolMessage(c.writer, message)
}

func (c *Client) Initialize() error {
	return c.Send(&godap.InitializeRequest{
		Request: godap.Request{Command: "initialize"},
		Arguments: godap.InitializeRequestArguments{
			ClientID:                     "vscode",
			ClientName:                   "Visual Studio Code",
			AdapterID:                    "debugpy",
			Locale:                       "en",
			LinesStartAt1:                true,
			ColumnsStartAt1:              true,
			PathFormat:                   "path",
			SupportsVariableType:         true,
			SupportsVariablePaging:       true,
			SupportsRunInTerminalRequest: true,
			SupportsProgressReporting:    true,
			SupportsInvalidatedEvent:     true,
			SupportsMemoryReferences:     true,
			SupportsMemoryEvent:          true,
		},
	})
}

func (c *Client) Attach(args map[string]any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return c.Send(&godap.AttachRequest{Request: godap.Request{Command: "attach"}, Arguments: data})
}

func (c *Client) SetBreakpoints(source map[string]any, breakpoints []map[string]any) error {
	args := godap.SetBreakpointsArguments{Source: sourceFromMap(source)}
	for _, bp := range breakpoints {
		args.Breakpoints = append(args.Breakpoints, godap.SourceBreakpoint{Line: intValue(bp["line"]), Column: intValue(bp["column"]), Condition: stringValue(bp["condition"]), HitCondition: stringValue(bp["hitCondition"]), LogMessage: stringValue(bp["logMessage"])})
	}
	return c.Send(&godap.SetBreakpointsRequest{Request: godap.Request{Command: "setBreakpoints"}, Arguments: args})
}

func (c *Client) SetFunctionBreakpoints(breakpoints []godap.FunctionBreakpoint) error {
	return c.Send(&godap.SetFunctionBreakpointsRequest{Request: godap.Request{Command: "setFunctionBreakpoints"}, Arguments: godap.SetFunctionBreakpointsArguments{Breakpoints: breakpoints}})
}

func (c *Client) SetExceptionBreakpoints(filters []string) error {
	return c.Send(&godap.SetExceptionBreakpointsRequest{Request: godap.Request{Command: "setExceptionBreakpoints"}, Arguments: godap.SetExceptionBreakpointsArguments{Filters: filters}})
}

func (c *Client) ConfigurationDone() error {
	return c.Send(&godap.ConfigurationDoneRequest{Request: godap.Request{Command: "configurationDone"}, Arguments: &godap.ConfigurationDoneArguments{}})
}

func (c *Client) Threads() error {
	return c.Send(&godap.ThreadsRequest{Request: godap.Request{Command: "threads"}})
}

func (c *Client) StackTrace(threadID, startFrame, levels int) error {
	return c.Send(&godap.StackTraceRequest{Request: godap.Request{Command: "stackTrace"}, Arguments: godap.StackTraceArguments{ThreadId: threadID, StartFrame: startFrame, Levels: levels}})
}

func (c *Client) Scopes(frameID int) error {
	return c.Send(&godap.ScopesRequest{Request: godap.Request{Command: "scopes"}, Arguments: godap.ScopesArguments{FrameId: frameID}})
}

func (c *Client) Variables(ref int) error {
	return c.Send(&godap.VariablesRequest{Request: godap.Request{Command: "variables"}, Arguments: godap.VariablesArguments{VariablesReference: ref}})
}

func (c *Client) Evaluate(expression string, frameID int, context string) error {
	args := godap.EvaluateArguments{Expression: expression, Context: context}
	if frameID > 0 {
		args.FrameId = frameID
	}
	return c.Send(&godap.EvaluateRequest{Request: godap.Request{Command: "evaluate"}, Arguments: args})
}

func (c *Client) Continue(threadID int) error {
	return c.Send(&godap.ContinueRequest{Request: godap.Request{Command: "continue"}, Arguments: godap.ContinueArguments{ThreadId: threadID}})
}

func (c *Client) Next(threadID int) error {
	return c.Send(&godap.NextRequest{Request: godap.Request{Command: "next"}, Arguments: godap.NextArguments{ThreadId: threadID}})
}

func (c *Client) StepIn(threadID int) error {
	return c.Send(&godap.StepInRequest{Request: godap.Request{Command: "stepIn"}, Arguments: godap.StepInArguments{ThreadId: threadID}})
}

func (c *Client) StepOut(threadID int) error {
	return c.Send(&godap.StepOutRequest{Request: godap.Request{Command: "stepOut"}, Arguments: godap.StepOutArguments{ThreadId: threadID}})
}

func (c *Client) Pause(threadID int) error {
	return c.Send(&godap.PauseRequest{Request: godap.Request{Command: "pause"}, Arguments: godap.PauseArguments{ThreadId: threadID}})
}

func (c *Client) Disconnect() error {
	return c.Send(&godap.DisconnectRequest{Request: godap.Request{Command: "disconnect"}, Arguments: &godap.DisconnectArguments{}})
}

func sourceFromMap(source map[string]any) godap.Source {
	return godap.Source{Name: stringValue(source["name"]), Path: stringValue(source["path"]), SourceReference: intValue(source["sourceReference"])}
}

func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case uint64:
		return int(v)
	case uint:
		return int(v)
	default:
		return 0
	}
}
