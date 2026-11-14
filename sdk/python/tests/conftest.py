import os
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request

import pytest

from . import fake_upstream

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


@pytest.fixture(scope="session")
def binary():
    exe = os.path.join(ROOT, "bin", "rungated")
    subprocess.run(["go", "build", "-o", exe, "./cmd/rungated"], cwd=ROOT, check=True)
    return exe


@pytest.fixture(scope="session")
def upstream():
    srv, handler = fake_upstream.start(free_port())
    yield srv, handler
    srv.shutdown()


@pytest.fixture()
def gateway(binary, upstream):
    srv, handler = upstream
    handler.seen.clear()
    port = free_port()
    data = tempfile.mkdtemp(prefix="ab-sdk-")
    base = "http://127.0.0.1:%d" % srv.server_address[1]
    env = dict(os.environ, RUNGATED_OPENAI_BASE_URL=base + "/v1", RUNGATED_ANTHROPIC_BASE_URL=base + "/v1", OPENAI_API_KEY="sk-up", ANTHROPIC_API_KEY="ak-up")
    proc = subprocess.Popen([binary, "serve", "--dev", "--listen", "127.0.0.1:%d" % port, "--data", data], cwd=ROOT, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    url = "http://127.0.0.1:%d" % port
    for _ in range(150):
        try:
            urllib.request.urlopen(url + "/v1/health", timeout=1)
            break
        except Exception:
            time.sleep(0.1)
    else:
        proc.kill()
        raise RuntimeError("gateway did not start")
    yield url, handler
    proc.terminate()
    proc.wait(timeout=10)
    shutil.rmtree(data, ignore_errors=True)


@pytest.fixture()
def ab(gateway):
    from rungated import Rungated

    url, _ = gateway
    c = Rungated(gateway=url, agent_id="finance-agent", session_id="sdk-test")
    yield c
    c.close()


@pytest.fixture()
def approver(gateway):
    from rungated import Rungated

    url, _ = gateway
    with open(os.path.join(ROOT, "spec", "ats", "examples", "finance-agent.yaml"), "r", encoding="utf-8") as f:
        text = f.read()
    text = text.replace("id: finance-agent", "id: sdk-approver", 1)
    block = "    - name: transfer\n      server: finance-mcp\n      blastRadius: high\n      requireApproval: true\n      args:\n        amount: {type: number, required: true, max: 5000}\n"
    text = text.replace("    - name: send_email\n", block + "    - name: send_email\n", 1)
    admin = Rungated(gateway=url)
    admin.register(text)
    c = Rungated(gateway=url, agent_id="sdk-approver", session_id="appr", hold_timeout=30)
    yield c, admin, url
    c.close()
    admin.close()
