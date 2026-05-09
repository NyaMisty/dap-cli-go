# dap-cli-go

Go port of `dap-cli` with a persistent daemon, msgpack IPC, and debugpy DAP attach support.

## Features

- `dap` CLI + `dap-daemon`
- connect/listen attach for debugpy
- persistent daemon with endpoint discovery
- structured logs + optional DAP protocol verbose output
- stack / scopes / vars / eval / breakpoints
- tested with debugpy connect and listen flows

## Install

### From source

```bash
go install github.com/NyaMisty/dap-cli-go/cmd/dap@latest
go install github.com/NyaMisty/dap-cli-go/cmd/dap-daemon@latest
```

## Minimal debug example

Start a Python target with debugpy:

```bash
debugpy --listen 127.0.0.1:2455 --wait-for-client -m http.server 48327
```

Attach with `dap`:

```bash
dap attach --connect-host 127.0.0.1 --connect-port 2455
```

Query status / threads:

```bash
dap status
dap threads
```

Enable DAP packet tracing when needed:

```bash
dap --dap-verbose attach --connect-host 127.0.0.1 --connect-port 2455
```

## Common commands

```bash
dap status
dap break path/to/file.py 123
dap continue
dap pause
dap stack
dap scopes
dap vars 1001
dap eval "1 + 1"
dap shutdown
```

## CI and release

- Every push to `master` / `main` builds `dap` and `dap-daemon`, runs `go test ./...`, and uploads build artifacts in GitHub Actions.
- Git tags matching `v*` trigger GoReleaser to publish release archives and checksums.
