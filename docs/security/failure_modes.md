# Failure modes

| Situation | What happens |
| --- | --- |
| Gateway unreachable (SDK) | Fails closed by default: the SDK raises `GatewayError` and the action does not run. `fail_open` / `failOpen` lets it through. |
| Gateway unreachable (LLM proxy) | The agent's request fails. Nothing reaches the provider if the proxy is the only route. |
| Gateway restarted | Serves again. Containment persists. The audit chain still verifies. |
| Detector or Tier 1 model error | With `risk.failMode: closed` (default) the action is raised to at least `hold`. With `open` it continues on the remaining findings. |
| LLM judge unavailable | A hold stays a hold. |
| Approval not answered | Follows `escalation.onTimeout`, deny by default. |
| Postgres or NATS unavailable at start | The gateway refuses to start. |
| Process killed within milliseconds of a decision | The decision was enforced, but its audit record can be lost with the local bus. Use JetStream. |

Fail closed is the right choice for agents that touch customer or production data. Run two or more replicas with Postgres and JetStream so one failure does not stop agents.
