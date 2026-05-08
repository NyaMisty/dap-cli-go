from __future__ import annotations

import os
import socket
import subprocess
import sys
import textwrap
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
GO_ROOT = ROOT.parent
FIXTURE = ROOT / "fixtures" / "simple_http_server.py"


def test_go_dap_attach_connect_e2e(tmp_path: Path) -> None:
    debug_port = _free_port()
    script = tmp_path / "connect_target.py"
    script.write_text(
        textwrap.dedent(
            f'''
            import debugpy
            import time

            debugpy.listen(("127.0.0.1", {debug_port}))
            print("READY", flush=True)
            debugpy.wait_for_client()

            def busy_loop():
                counter = 0
                while True:
                    counter += 1
                    time.sleep(0.05)

            busy_loop()
            '''
        ),
        encoding="utf-8",
    )
    process = _start_process([sys.executable, str(script)])
    dap_exe = _build_dap_exe(tmp_path)
    runtime_dir = tmp_path / "runtime"
    daemon = None
    try:
        _wait_for_stdout_line(process, "READY")
        daemon = _start_daemon(dap_exe, runtime_dir)
        _wait_for_file(_endpoint_path(runtime_dir))
        _run_go(dap_exe, ["attach", "--connect-host", "127.0.0.1", "--connect-port", str(debug_port)], runtime_dir=runtime_dir)
        status = _wait_for_status(dap_exe, "running", runtime_dir=runtime_dir, timeout=20)
        assert "running" in status
        threads = _run_go(dap_exe, ["threads"], runtime_dir=runtime_dir)
        assert "MainThread" in threads
    finally:
        _run_go(dap_exe, ["shutdown"], runtime_dir=runtime_dir, allow_fail=True)
        _terminate(process)
        if daemon is not None:
            _terminate(daemon)


def test_go_dap_attach_listen_e2e(tmp_path: Path) -> None:
    listen_port = _free_port()
    script = tmp_path / "listen_target.py"
    script.write_text(
        textwrap.dedent(
            f'''
            import debugpy
            import time

            debugpy.connect(("127.0.0.1", {listen_port}))
            debugpy.wait_for_client()

            def busy_loop():
                counter = 0
                while True:
                    counter += 1
                    time.sleep(0.05)

            busy_loop()
            '''
        ),
        encoding="utf-8",
    )
    dap_exe = _build_dap_exe(tmp_path)
    runtime_dir = tmp_path / "runtime"
    daemon = _start_daemon(dap_exe, runtime_dir)
    try:
        _wait_for_file(_endpoint_path(runtime_dir))
        _run_go(dap_exe, ["attach", "--listen-host", "127.0.0.1", "--listen-port", str(listen_port)], runtime_dir=runtime_dir)
        process = _start_process([sys.executable, str(script)])
        try:
            status = _wait_for_status(dap_exe, ("running", "stopped"), runtime_dir=runtime_dir, timeout=20)
            assert "running" in status or "stopped" in status
        finally:
            _terminate(process)
    finally:
        _run_go(dap_exe, ["shutdown"], runtime_dir=runtime_dir, allow_fail=True)
        _terminate(daemon)


def _wait_for_status(dap_exe: Path, expected, runtime_dir: Path, timeout: float = 20.0) -> str:
    if isinstance(expected, str):
        expected = (expected,)
    deadline = time.time() + timeout
    last = ""
    while time.time() < deadline:
        last = _run_go(dap_exe, ["status"], runtime_dir=runtime_dir, allow_fail=True)
        if any(state in last for state in expected):
            return last
        time.sleep(0.5)
    raise AssertionError(f"timed out waiting for status {expected!r}; last output:\n{last}")


def _run_go(dap_exe: Path, args: list[str], runtime_dir: Path | None = None, allow_fail: bool = False) -> str:
    command = [str(dap_exe), *args]
    env = os.environ.copy()
    if runtime_dir is not None:
        env["DAP_CLI_RUNTIME_DIR"] = str(runtime_dir)
    result = subprocess.run(command, cwd=str(GO_ROOT), capture_output=True, text=True, env=env)
    if not allow_fail and result.returncode != 0:
        raise AssertionError(f"command failed: {' '.join(command)}\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}")
    return result.stdout + result.stderr


def _build_dap_exe(tmp_path: Path) -> Path:
    dap_exe = tmp_path / "dap-e2e.exe"
    subprocess.run(["go", "build", "-o", str(dap_exe), "./cmd/dap"], cwd=str(GO_ROOT), check=True)
    return dap_exe


def _project_key(root: Path) -> str:
    return str(root.resolve()).replace(":", "_").replace("\\", "_").replace("/", "_")


def _endpoint_path(runtime_dir: Path) -> Path:
    return runtime_dir / "runtime" / f"{_project_key(GO_ROOT)}.json"


def _start_daemon(dap_exe: Path, runtime_dir: Path) -> subprocess.Popen[str]:
    endpoint = _endpoint_path(runtime_dir)
    endpoint.parent.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env["DAP_CLI_RUNTIME_DIR"] = str(runtime_dir)
    return subprocess.Popen(
        [str(dap_exe), "daemon", "--root", str(GO_ROOT), "--endpoint", str(endpoint)],
        cwd=str(GO_ROOT),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
    )


def _free_port() -> int:
    for port in range(42000, 52000):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            try:
                sock.bind(("127.0.0.1", port))
            except OSError:
                continue
            return port
    raise RuntimeError("no free port found")


def _wait_for_stdout_line(process: subprocess.Popen[str], needle: str, timeout: float = 10.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        line = process.stdout.readline() if process.stdout else ""
        if needle in line:
            return
        if process.poll() is not None:
            stderr = process.stderr.read() if process.stderr else ""
            raise AssertionError(f"Process exited early with {process.returncode}: {stderr}")
        time.sleep(0.05)
    raise TimeoutError(f"Timed out waiting for stdout line {needle!r}")


def _wait_for_file(path: Path, timeout: float = 10.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if path.exists():
            return
        time.sleep(0.1)
    raise TimeoutError(f"Timed out waiting for file {path}")


def _start_process(args: list[str]) -> subprocess.Popen[str]:
    return subprocess.Popen(args, cwd=str(GO_ROOT), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)


def _terminate(process: subprocess.Popen[str]) -> None:
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)
