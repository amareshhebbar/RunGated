# Write the rulebook from real traffic

Writing an agent's rulebook by hand is the slowest part of setup. You can skip most of it. Run your agent through Rungated in observe mode for a while, then let Rungated write the rulebook from what the agent actually did.

## 1. Install

```bash
curl -fsSL https://github.com/amareshhebbar/rungated/releases/latest/download/install.sh | sh
```

Linux and macOS, Intel and ARM. Windows builds are on the releases page.

## 2. Start the gateway so it records unknown agents

```bash
RUNGATED_API_KEY=change-me RUNGATED_UNKNOWN_AGENTS=observe rungated serve
```

`RUNGATED_UNKNOWN_AGENTS=observe` lets an agent with no rulebook through and records everything it does. Without it, unknown agents are blocked.

If the agent is already registered, set its mode to `observe` instead. Calls outside its rulebook are then recorded and allowed.

## 3. Run your agent as usual

Point the SDK at the gateway (see the [LangGraph](langgraph.md), [OpenAI Agents](openai_agents.md) or [Vercel AI SDK](vercel_ai_sdk.md) guide) and run your normal workload: tests, a staging day, a demo script. The more normal traffic it sees, the better the rulebook.

## 4. Write the rulebook

```bash
rungated init --id support-agent \
  --owner you@example.com --purpose "Looks up orders and issues refunds" \
  --from-gateway http://localhost:8080 --api-key change-me
```

This writes `rungated.agent.yaml` with:

| What it saw | What it writes |
|---|---|
| Each tool called | A tool entry with its server and how often it was seen |
| Arguments present on every call | `required: true` |
| Numbers | `type`, and `min` and `max` with 50 percent headroom over what was seen |
| Strings with one fixed shape, like `ORD-12345` | A `pattern`, here `^ORD-[0-9]{5}$` |
| Strings with a few repeated values over 20 or more calls | An `enum` |
| Other strings | A `maxLength` |
| Tool names with words like delete, refund, send, pay | `blastRadius: high` |
| Tool names with words like get, list, search | `blastRadius: low` |
| Models used | `model.allowed`, with dated versions folded to a wildcard |
| Hosts reached through the egress proxy | `egress.allow` |
| Sensitive data types seen | `dataClasses.allowed` |

Calls that Rungated flagged as an attack or a leaked secret are left out, so an attack during the learning window does not become allowed.

No gateway running? Learn from the events file in the data folder instead:

```bash
rungated init --id support-agent --from-events ./data/state/events.jsonl
```

## 5. Review, register, enforce

Read the file. Tighten anything that is wider than it should be, and delete any tool the agent should never use, even if it was seen. Then:

```bash
rungated validate rungated.agent.yaml
rungated agents register rungated.agent.yaml --gateway http://localhost:8080 --api-key change-me
```

The rulebook starts in `observe`. Watch the Console for a day. When it stops showing calls you consider normal as out of scope, change `autonomy.default` to `auto_contain` and register it again. From then on, a refund over the learned limit, an argument that does not match its shape, or a tool not in the list is blocked.

## Starting from nothing

`rungated init --id my-agent` with no traffic flags writes a starter rulebook with two example tools to edit.
