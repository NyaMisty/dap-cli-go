package cli

import (
	"fmt"
	"strings"
)

func renderSnapshot(snapshot map[string]any) string {
	lifecycle := defaultString(stringValue(snapshot["lifecycle"]), "unknown")
	adapter := defaultString(stringValue(snapshot["adapter_id"]), "unconfigured")
	program := stringValue(snapshot["program"])
	if program == "" {
		program = stringValue(snapshot["root"])
	}
	if program == "" {
		program = "no target"
	}
	pid := displayValue(snapshot["process_id"])
	clients := len(sliceMapValue(snapshot["clients"]))
	thread := displayValue(snapshot["thread_id"])
	frame := displayValue(snapshot["frame_id"])
	source := defaultString(stringValue(snapshot["source_path"]), "-")
	line := displayValue(snapshot["line"])
	reason := defaultString(stringValue(snapshot["stop_reason"]), "-")
	bpCount := len(sliceMapValue(snapshot["breakpoints"]))
	rows := []string{
		fmt.Sprintf("Session %v | %s | %s | PID %s | clients %d", snapshot["session_id"], adapter, lifecycle, pid, clients),
		fmt.Sprintf("Target  %s", program),
		fmt.Sprintf("Focus   thread %s | frame %s | %s:%s | stop %s | breakpoints %d", thread, frame, source, line, reason, bpCount),
	}
	if output := latestOutput(snapshot); output != "" {
		rows = append(rows, "Output  "+output)
	}
	return strings.Join(rows, "\n")
}

func renderThreads(snapshot map[string]any) string {
	threads := sliceMapValue(snapshot["threads"])
	if len(threads) == 0 {
		return "No threads known."
	}
	rows := make([]string, 0, len(threads))
	for _, thread := range threads {
		rows = append(rows, fmt.Sprintf("%v: %v", thread["id"], thread["name"]))
	}
	return strings.Join(rows, "\n")
}

func renderStack(snapshot map[string]any) string {
	frames := sliceMapValue(snapshot["stack_frames"])
	stackTrace := mapValue(snapshot["last_stack_trace"])
	if len(frames) == 0 {
		if completed, ok := stackTrace["completed"].(bool); ok && !completed {
			return "Stack trace request is still pending."
		}
		if success, ok := stackTrace["success"].(bool); ok && !success {
			message := defaultString(stringValue(stackTrace["message"]), "adapter returned an error")
			return "Stack trace failed: " + message
		}
		if completed, ok := stackTrace["completed"].(bool); ok && completed {
			return "Stack trace completed: no stack frames available for the selected thread."
		}
		return "No stack trace has been requested yet."
	}
	rows := make([]string, 0, len(frames))
	for _, frame := range frames {
		source := mapValue(frame["source"])
		path := firstString(source["path"], source["name"], "-")
		rows = append(rows, fmt.Sprintf("#%v %v %s:%v", frame["id"], frame["name"], path, frame["line"]))
	}
	return strings.Join(rows, "\n")
}

func renderBreakpoints(snapshot map[string]any) string {
	breakpoints := sliceMapValue(snapshot["breakpoints"])
	if len(breakpoints) == 0 {
		return "No breakpoints set."
	}
	rows := make([]string, 0, len(breakpoints))
	for _, bp := range breakpoints {
		source := mapValue(bp["source"])
		path := firstString(source["path"], source["name"], bp["path"], "-")
		status := "verified"
		if verified, ok := bp["verified"].(bool); ok && !verified {
			status = "pending"
		}
		rows = append(rows, fmt.Sprintf("%s:%v %s", path, bp["line"], status))
	}
	return strings.Join(rows, "\n")
}

func latestOutput(snapshot map[string]any) string {
	outputs := sliceMapValue(snapshot["recent_output"])
	if len(outputs) == 0 {
		return ""
	}
	latest := outputs[len(outputs)-1]
	text := strings.TrimSpace(firstString(latest["output"], latest["text"]))
	if len(text) > 120 {
		return text[:117] + "..."
	}
	return text
}

func displayValue(value any) string {
	if value == nil || fmt.Sprint(value) == "" || fmt.Sprint(value) == "<nil>" {
		return "-"
	}
	return fmt.Sprint(value)
}
