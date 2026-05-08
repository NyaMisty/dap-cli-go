package logx

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type Options struct {
	Level  string
	Format string
	Output io.Writer
}

func New(opts Options) (*slog.Logger, error) {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}
	output := opts.Output
	if output == nil {
		output = io.Discard
	}
	handlerOpts := &slog.HandlerOptions{Level: level}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(output, handlerOpts)
	case "json":
		handler = slog.NewJSONHandler(output, handlerOpts)
	default:
		return nil, fmt.Errorf("unsupported log format %q", opts.Format)
	}
	return slog.New(handler), nil
}

func parseLevel(value string) (slog.Leveler, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return nil, fmt.Errorf("unsupported log level %q", value)
	}
}
