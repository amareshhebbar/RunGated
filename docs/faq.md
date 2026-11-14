# FAQ

**Does it stop all prompt injection?** No. It enforces declared policy and detects known patterns. See [Limits](security/limits.md).

**Does it add latency?** Tier 0 decisions take well under a millisecond on a laptop. Measure on your own hardware.

**Do agents see my provider keys?** No. Keys stay on the gateway.

**What if the gateway goes down?** Fail closed by default. See [Failure modes](security/failure_modes.md).

**Can I run it without internet?** Yes. The Free build needs no network, and the free models run locally. Enterprise adds a signed offline bundle, see [Air gapped install](enterprise/airgap.md).

**Which frameworks work?** Anything that uses an OpenAI or Anthropic compatible client, plus adapters for LangGraph, LangChain, the OpenAI Agents SDK and the Vercel AI SDK.
