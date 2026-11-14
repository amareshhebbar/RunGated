import os
import shutil
import socket
import subprocess
import tempfile
import time

import httpx
import pytest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
BIN = os.environ.get("RUNGATED_BIN", os.path.join(ROOT, "bin", "rungated"))
MANIFEST = os.path.join(ROOT, "spec", "ats", "examples", "finance-agent.yaml")
ADMIN_KEY = "e2e-admin-key"


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def wait_http(url, timeout=15):
    end = time.time() + timeout
    while time.time() < end:
        try:
            if httpx.get(url, timeout=1, trust_env=False).status_code < 500:
                return
        except Exception:
            time.sleep(0.1)
    raise RuntimeError("timeout waiting for " + url)


class Stack:
    def __init__(self, secured=False, manifest=MANIFEST, agent_id="finance-agent"):
        self.secured = secured
        self.src_manifest = manifest
        self.agent_id = agent_id
        self.data = tempfile.mkdtemp(prefix="rg-e2e-")
        self.procs = []
        self.llm_port, self.mcp_port, self.web_port = free_port(), free_port(), free_port()
        self.gw_port, self.eg_port = free_port(), free_port()
        self.gateway = "http://127.0.0.1:%d" % self.gw_port
        self.egress = "http://127.0.0.1:%d" % self.eg_port
        self.llm = "http://127.0.0.1:%d" % self.llm_port
        self.mcp = "http://127.0.0.1:%d" % self.mcp_port
        self.web = "http://127.0.0.1:%d" % self.web_port
        self.agent_key = None
        self.http = httpx.Client(timeout=15, trust_env=False)
        self.manifest = os.path.join(self.data, "finance-agent.yaml")
        text = open(self.src_manifest).read().replace("localhost:9301", "127.0.0.1:%d" % self.mcp_port)
        open(self.manifest, "w").write(text)

    def spawn(self, args, env=None):
        e = dict(os.environ)
        e.update(env or {})
        p = subprocess.Popen([BIN] + args, env=e, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.procs.append(p)
        return p

    def start_gateway(self):
        env = {
            "RUNGATED_OPENAI_BASE_URL": self.llm + "/v1",
            "OPENAI_API_KEY": "demo",
            "RUNGATED_EGRESS_LISTEN": "127.0.0.1:%d" % self.eg_port,
        }
        args = ["serve", "--listen", "127.0.0.1:%d" % self.gw_port, "--data", self.data]
        if self.secured:
            env["RUNGATED_API_KEY"] = ADMIN_KEY
            env["RUNGATED_REQUIRE_AGENT_AUTH"] = "true"
        else:
            args += ["--dev", "--manifests", self.manifest]
        self.gw = self.spawn(args, env)
        wait_http(self.gateway + "/v1/health")

    def start(self):
        self.spawn(["demo", "mock-llm", "--listen", "127.0.0.1:%d" % self.llm_port])
        self.spawn(["demo", "poisoned-mcp", "--listen", "127.0.0.1:%d" % self.mcp_port])
        self.spawn(["demo", "evil-web", "--listen", "127.0.0.1:%d" % self.web_port])
        for u in (self.llm, self.mcp, self.web):
            wait_http(u + "/healthz")
        self.start_gateway()
        if self.secured:
            r = self.http.post(self.gateway + "/v1/agents", content=open(self.manifest, "rb").read(), headers=self.admin())
            assert r.status_code in (200, 201), r.text
            j = r.json()
            self.agent_key = j.get("agent_key") or j.get("key") or j.get("api_key")
            assert self.agent_key, j
        return self

    def stop_gateway(self):
        self.gw.terminate()
        self.gw.wait(10)

    def admin(self):
        return {"Authorization": "Bearer " + ADMIN_KEY}

    def agent(self, session=None):
        h = {"X-Rungated-Agent": self.agent_id}
        if self.secured:
            h["X-Rungated-Key"] = self.agent_key
        if session:
            h["X-Rungated-Session"] = session
        return h

    def decide(self, body):
        b = {"agent_id": self.agent_id}
        b.update(body)
        h = self.agent() if self.secured else {}
        return self.http.post(self.gateway + "/v1/decide", json=b, headers=h).json()

    def reset_demo(self):
        for u in (self.llm, self.mcp, self.web):
            self.http.post(u + "/_demo/reset")

    def admin_headers(self):
        return self.admin() if self.secured else {}

    def release(self):
        self.http.post(self.gateway + "/v1/release", json={"agent_id": self.agent_id}, headers=self.admin_headers())

    def proxied(self):
        cred = self.agent_id + ":" + (self.agent_key or "")
        return httpx.Client(proxy=self.egress.replace("http://", "http://" + cred + "@"), timeout=15, trust_env=False)

    def close(self):
        for p in self.procs:
            try:
                p.kill()
                p.wait(5)
            except Exception:
                pass
        shutil.rmtree(self.data, ignore_errors=True)


@pytest.fixture(scope="session")
def dev():
    s = Stack(False).start()
    yield s
    s.close()


@pytest.fixture(scope="session")
def secured():
    s = Stack(True).start()
    yield s
    s.close()


@pytest.fixture(autouse=True)
def _clean(request):
    for name in ("dev", "secured"):
        if name in request.fixturenames:
            s = request.getfixturevalue(name)
            s.release()
            s.reset_demo()
    yield
