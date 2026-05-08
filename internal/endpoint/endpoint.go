package endpoint

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NyaMisty/dap-cli-go/internal/model"
)

type Info struct {
	Path  string `json:"path" msgpack:"path"`
	Host  string `json:"host" msgpack:"host"`
	Port  int    `json:"port" msgpack:"port"`
	Token string `json:"token" msgpack:"token"`
	PID   int    `json:"pid,omitempty" msgpack:"pid,omitempty"`
}

func FindProjectRoot(start string) (string, error) {
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		start = cwd
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if exists(filepath.Join(abs, "pyproject.toml")) || exists(filepath.Join(abs, ".git")) {
			return filepath.EvalSymlinks(abs)
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return filepath.EvalSymlinks(start)
		}
		abs = parent
	}
}

func RuntimeRoot() (string, error) {
	base := os.Getenv("DAP_CLI_RUNTIME_DIR")
	if base == "" {
		if runtime.GOOS != "windows" {
			if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
				base = filepath.Join(xdg, "dap-cli")
			}
		}
	}
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(cache, "dap-cli")
	}
	path := filepath.Join(base, "runtime")
	return path, os.MkdirAll(path, 0o700)
}

func EndpointPath(root string) (string, error) {
	runtimeRoot, err := RuntimeRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(runtimeRoot, ProjectKey(root)+".json"), nil
}

func ProjectKey(root string) string {
	abs, err := filepath.Abs(root)
	if err == nil {
		root = abs
	}
	root = strings.ReplaceAll(root, ":", "_")
	root = strings.ReplaceAll(root, "\\", "_")
	root = strings.ReplaceAll(root, "/", "_")
	return root
}

func Discover(root string) (Info, error) {
	resolved, err := FindProjectRoot(root)
	if err != nil {
		return Info{}, err
	}
	path, err := EndpointPath(resolved)
	if err != nil {
		return Info{}, err
	}
	return Info{Path: path, Host: "127.0.0.1"}, nil
}

func NewInfo(root, host string, port int) (Info, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	resolved, err := FindProjectRoot(root)
	if err != nil {
		return Info{}, err
	}
	path, err := EndpointPath(resolved)
	if err != nil {
		return Info{}, err
	}
	token, err := model.NewToken()
	if err != nil {
		return Info{}, err
	}
	return Info{Path: path, Host: host, Port: port, Token: token, PID: os.Getpid()}, nil
}

func Read(root string) (Info, error) {
	resolved, err := FindProjectRoot(root)
	if err != nil {
		return Info{}, err
	}
	path, err := EndpointPath(resolved)
	if err != nil {
		return Info{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Info{}, os.ErrNotExist
	}
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, err
	}
	if info.Path == "" {
		info.Path = path
	}
	return info, nil
}

func Write(root string, info Info) error {
	resolved, err := FindProjectRoot(root)
	if err != nil {
		return err
	}
	path, err := EndpointPath(resolved)
	if err != nil {
		return err
	}
	if info.Path == "" {
		info.Path = path
	}
	if err := os.MkdirAll(filepath.Dir(info.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(info.Path, data, 0o600)
}

func Remove(root string) error {
	resolved, err := FindProjectRoot(root)
	if err != nil {
		return err
	}
	path, err := EndpointPath(resolved)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func Reachable(info Info) bool {
	if info.Port <= 0 || info.Host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(info.Host, intString(info.Port)), defaultDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
