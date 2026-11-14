# Limits

Be clear about what Rungated does and does not do.

* It enforces the policy you declare and detects known attack patterns. It does not stop prompt injection in general.
* It cannot protect an agent that bypasses it. Block direct network access so the gateway is the only route.
* It does not judge whether an answer is true.
* The built in benchmark corpus was written alongside the Tier 0 rules, so its recall is optimistic. Measure on your own traffic before you rely on it.
* Tier 1 model quality depends on the model you train or supply.
* HTTPS body inspection needs the Rungated CA installed in the agent.
* The audit chain is tamper evident only if you store its head hash outside the system.
* Network partitions between the gateway and its store, a full disk and clock skew are not covered by automated tests.
