# acme-support

A small customer support agent that lives outside the Rungated code base and uses Rungated the way a customer would: install the SDK, write a manifest, point the agent at the gateway.

```bash
./run.sh
```

The script builds nothing in this folder. It installs the SDK and this project into a fresh virtualenv, starts the gateway with authentication on and the mock model, registers `rungated.agent.yaml`, then runs `check.py`, which asserts that allowed calls run and unsafe calls are blocked or redacted. It exits 0 only if every check passes.
