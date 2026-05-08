package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/NyaMisty/dap-cli-go/internal/daemon"
	"github.com/NyaMisty/dap-cli-go/internal/endpoint"
	"github.com/NyaMisty/dap-cli-go/internal/logx"
	"github.com/spf13/cobra"
)

type Options struct {
	Root       string
	LogLevel   string
	LogFormat  string
	DAPVerbose bool
}

func NewRootCommand() *cobra.Command {
	opts := &Options{LogLevel: "info", LogFormat: "text"}
	cmd := &cobra.Command{
		Use:           "dap",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithApp(opts, "status", nil, func(app *ClientApp) error { _, err := app.RunCommand("status", nil); return err })
		},
	}
	cmd.PersistentFlags().StringVar(&opts.Root, "root", "", "project root")
	cmd.PersistentFlags().StringVar(&opts.LogLevel, "log-level", "info", "log level: debug, info, warn, error")
	cmd.PersistentFlags().StringVar(&opts.LogFormat, "log-format", "text", "log format: text or json")
	cmd.PersistentFlags().BoolVar(&opts.DAPVerbose, "dap-verbose", false, "print DAP protocol packets")

	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if opts.DAPVerbose {
			_ = os.Setenv("DAP_CLI_DAP_VERBOSE", "1")
		}
		return nil
	}
	cmd.AddCommand(statusCommand(opts))
	cmd.AddCommand(attachCommand(opts))
	cmd.AddCommand(simpleCommand(opts, "continue", "debug.continue"))
	cmd.AddCommand(simpleCommand(opts, "pause", "debug.pause"))
	cmd.AddCommand(simpleCommand(opts, "step", "debug.step"))
	cmd.AddCommand(simpleCommand(opts, "step-in", "debug.step_in"))
	cmd.AddCommand(simpleCommand(opts, "step-out", "debug.step_out"))
	cmd.AddCommand(threadsCommand(opts))
	cmd.AddCommand(stackCommand(opts))
	cmd.AddCommand(scopesCommand(opts))
	cmd.AddCommand(varsCommand(opts))
	cmd.AddCommand(evalCommand(opts))
	cmd.AddCommand(breakCommand(opts))
	cmd.AddCommand(clearBreaksCommand(opts))
	cmd.AddCommand(simpleCommand(opts, "stop", "session.stop"))
	cmd.AddCommand(simpleCommand(opts, "shutdown", "daemon.shutdown"))
	cmd.AddCommand(monitorCommand(opts))
	cmd.AddCommand(shellCommand(opts))
	cmd.AddCommand(daemonCommand(opts))
	return cmd
}

func statusCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
		return runWithApp(opts, "status", nil, func(app *ClientApp) error { _, err := app.RunCommand("status", nil); return err })
	}}
}

func attachCommand(opts *Options) *cobra.Command {
	var connectHost, listenHost string
	var connectPort, listenPort int
	cmd := &cobra.Command{Use: "attach", RunE: func(cmd *cobra.Command, args []string) error {
		if connectPort != 0 && listenPort != 0 {
			return fmt.Errorf("--connect-port and --listen-port are mutually exclusive")
		}
		if connectPort == 0 && listenPort == 0 {
			return fmt.Errorf("attach requires either --connect-port or --listen-port")
		}
		attach := map[string]any{"type": "debugpy", "request": "attach"}
		if connectPort != 0 {
			attach["mode"] = "connect"
			attach["connect"] = map[string]any{"host": connectHost, "port": connectPort}
		} else {
			attach["mode"] = "listen"
			attach["listen"] = map[string]any{"host": listenHost, "port": listenPort}
		}
		return runWithApp(opts, "attach", map[string]any{"attach": attach}, func(app *ClientApp) error {
			_, err := app.RunCommand("attach", map[string]any{"attach": attach})
			return err
		})
	}}
	cmd.Flags().StringVar(&connectHost, "connect-host", "127.0.0.1", "debugpy connect host")
	cmd.Flags().IntVar(&connectPort, "connect-port", 0, "debugpy connect port")
	cmd.Flags().StringVar(&listenHost, "listen-host", "127.0.0.1", "debugpy listen host")
	cmd.Flags().IntVar(&listenPort, "listen-port", 0, "debugpy listen port")
	return cmd
}

func breakCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "break PATH LINE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		line, err := strconv.Atoi(args[1])
		if err != nil {
			return err
		}
		return runWithApp(opts, "break", map[string]any{"path": args[0], "line": line}, func(app *ClientApp) error {
			snapshot, err := app.RunCommand("break", map[string]any{"path": args[0], "line": line})
			if err == nil {
				fmt.Println(renderBreakpoints(snapshot))
			}
			return err
		})
	}}
}

func clearBreaksCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "clear-breaks PATH", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runWithApp(opts, "clear-breaks", map[string]any{"path": args[0]}, func(app *ClientApp) error {
			_, err := app.RunCommand("clear-breaks", map[string]any{"path": args[0]})
			return err
		})
	}}
}

func threadsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "threads", RunE: func(cmd *cobra.Command, args []string) error {
		return runWithApp(opts, "threads", nil, func(app *ClientApp) error {
			snapshot, err := app.RunCommand("threads", nil)
			if err == nil {
				fmt.Println(renderThreads(snapshot))
			}
			return err
		})
	}}
}

func stackCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "stack", RunE: func(cmd *cobra.Command, args []string) error {
		return runWithApp(opts, "stack", nil, func(app *ClientApp) error {
			snapshot, err := app.RunCommand("stack", nil)
			if err == nil {
				fmt.Println(renderStack(snapshot))
			}
			return err
		})
	}}
}

func scopesCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "scopes", RunE: func(cmd *cobra.Command, args []string) error {
		return runWithApp(opts, "scopes", nil, func(app *ClientApp) error { _, err := app.RunCommand("scopes", nil); return err })
	}}
}

func varsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "vars REFERENCE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ref, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		return runWithApp(opts, "vars", map[string]any{"variables_reference": ref}, func(app *ClientApp) error {
			_, err := app.RunCommand("vars", map[string]any{"variables_reference": ref})
			return err
		})
	}}
}

func evalCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "eval EXPRESSION", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runWithApp(opts, "eval", map[string]any{"expression": args[0]}, func(app *ClientApp) error {
			_, err := app.RunCommand("eval", map[string]any{"expression": args[0]})
			return err
		})
	}}
}

func simpleCommand(opts *Options, name, requestType string) *cobra.Command {
	return &cobra.Command{Use: name, RunE: func(cmd *cobra.Command, args []string) error {
		_ = requestType
		return runWithApp(opts, name, nil, func(app *ClientApp) error { _, err := app.RunCommand(name, nil); return err })
	}}
}

func monitorCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "monitor", RunE: func(cmd *cobra.Command, args []string) error {
		root, app, err := connectApp(opts)
		if err != nil {
			return err
		}
		_ = root
		defer app.Close()
		_, err = app.RunCommand("status", nil)
		if err != nil {
			return err
		}
		select {}
	}}
}

func shellCommand(opts *Options) *cobra.Command {
	return &cobra.Command{Use: "shell", RunE: func(cmd *cobra.Command, args []string) error {
		_, app, err := connectApp(opts)
		if err != nil {
			return err
		}
		defer app.Close()
		_, _ = app.RunCommand("status", nil)
		return runShell(app, os.Stdin)
	}}
}

func daemonCommand(opts *Options) *cobra.Command {
	var endpointPath, host string
	var port int
	cmd := &cobra.Command{Use: "daemon", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := logx.New(logx.Options{Level: opts.LogLevel, Format: opts.LogFormat, Output: os.Stderr})
		if err != nil {
			return err
		}
		root, err := endpoint.FindProjectRoot(opts.Root)
		if err != nil {
			return err
		}
		if endpointPath == "" {
			ep, err := endpoint.Discover(root)
			if err != nil {
				return err
			}
			endpointPath = ep.Path
		}
		return daemon.Serve(root, endpointPath, host, port, logger, opts.DAPVerbose)
	}}
	cmd.Flags().StringVar(&endpointPath, "endpoint", "", "endpoint path")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "listen host")
	cmd.Flags().IntVar(&port, "port", 0, "listen port")
	return cmd
}

func NewDaemonCommand() *cobra.Command {
	opts := &Options{LogLevel: "info", LogFormat: "text"}
	var endpointPath, host string
	var port int
	cmd := &cobra.Command{Use: "dap-daemon", RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := logx.New(logx.Options{Level: opts.LogLevel, Format: opts.LogFormat, Output: os.Stderr})
		if err != nil {
			return err
		}
		root, err := endpoint.FindProjectRoot(opts.Root)
		if err != nil {
			return err
		}
		return daemon.Serve(root, endpointPath, host, port, logger, opts.DAPVerbose)
	}}
	cmd.Flags().StringVar(&opts.Root, "root", "", "project root")
	cmd.Flags().StringVar(&endpointPath, "endpoint", "", "endpoint path")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "listen host")
	cmd.Flags().IntVar(&port, "port", 0, "listen port")
	cmd.Flags().StringVar(&opts.LogLevel, "log-level", "info", "log level")
	cmd.Flags().StringVar(&opts.LogFormat, "log-format", "text", "log format")
	cmd.Flags().BoolVar(&opts.DAPVerbose, "dap-verbose", false, "print DAP protocol packets")
	_ = cmd.MarkFlagRequired("endpoint")
	return cmd
}

func runWithApp(opts *Options, command string, payload map[string]any, fn func(*ClientApp) error) error {
	_ = command
	_ = payload
	_, app, err := connectApp(opts)
	if err != nil {
		return err
	}
	defer app.Close()
	return fn(app)
}

func connectApp(opts *Options) (string, *ClientApp, error) {
	root, err := endpoint.FindProjectRoot(opts.Root)
	if err != nil {
		return "", nil, err
	}
	if opts.DAPVerbose {
		_ = os.Setenv("DAP_CLI_DAP_VERBOSE", "1")
	}
	ep, err := EnsureDaemon(root)
	if err != nil {
		return "", nil, err
	}
	app, err := NewClientApp(root, ep)
	if err != nil {
		return "", nil, err
	}
	return root, app, nil
}

func runShell(app *ClientApp, input io.Reader) error {
	scanner := bufio.NewScanner(input)
	for {
		fmt.Print("dap> ")
		if !scanner.Scan() {
			fmt.Println()
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		command := parts[0]
		switch {
		case command == "quit" || command == "exit" || command == "q":
			return nil
		case command == "status":
			_, _ = app.RunCommand("status", nil)
		case command == "attach" && len(parts) == 4 && (parts[1] == "connect" || parts[1] == "listen"):
			port, err := strconv.Atoi(parts[3])
			if err != nil {
				fmt.Println(err)
				continue
			}
			attach := map[string]any{"type": "debugpy", "request": "attach", "mode": parts[1], parts[1]: map[string]any{"host": parts[2], "port": port}}
			_, _ = app.RunCommand("attach", map[string]any{"attach": attach})
		case (command == "break" || command == "b") && len(parts) == 3:
			lineNo, err := strconv.Atoi(parts[2])
			if err != nil {
				fmt.Println(err)
				continue
			}
			_, _ = app.RunCommand("break", map[string]any{"path": parts[1], "line": lineNo})
		case command == "clear-breaks" && len(parts) == 2:
			_, _ = app.RunCommand("clear-breaks", map[string]any{"path": parts[1]})
		case command == "continue" || command == "c":
			_, _ = app.RunCommand("continue", nil)
		case command == "pause":
			_, _ = app.RunCommand("pause", nil)
		case command == "step" || command == "s":
			_, _ = app.RunCommand("step", nil)
		case command == "step-in" || command == "si":
			_, _ = app.RunCommand("step-in", nil)
		case command == "step-out" || command == "so":
			_, _ = app.RunCommand("step-out", nil)
		case command == "threads":
			_, _ = app.RunCommand("threads", nil)
		case command == "stack":
			_, _ = app.RunCommand("stack", nil)
		case command == "scopes":
			_, _ = app.RunCommand("scopes", nil)
		case command == "vars" && len(parts) == 2:
			ref, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println(err)
				continue
			}
			_, _ = app.RunCommand("vars", map[string]any{"variables_reference": ref})
		case command == "eval" && len(parts) >= 2:
			_, _ = app.RunCommand("eval", map[string]any{"expression": strings.TrimSpace(strings.TrimPrefix(line, command))})
		case command == "stop":
			_, _ = app.RunCommand("stop", nil)
		case command == "shutdown":
			_, _ = app.RunCommand("shutdown", nil)
		case command == "help":
			fmt.Println("status | attach connect <host> <port> | attach listen <host> <port> | break <path> <line> | clear-breaks <path> | continue | pause | step | step-in | step-out | threads | stack | scopes | vars <ref> | eval <expr> | stop | shutdown | quit")
		default:
			fmt.Println("Unknown command. Type 'help'.")
		}
	}
}
