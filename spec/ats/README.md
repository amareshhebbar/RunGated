# Agent Trust Specification

An agent manifest declares what an agent is allowed to do. Rungated compiles it to Cedar policy and uses it to scope tools, egress, data classes, limits and autonomy.

- manifest.v0.schema.json is the JSON Schema for editors and CI. The Go validator in pkg/manifest is the source of truth and adds cross field checks such as unknown server references, regex validity and threshold ordering.
- examples/ holds working manifests.

Editor support: add `# yaml-language-server: $schema=https://rungated.dev/schemas/ats/manifest.v0.schema.json` or point the schema setting at the local file.

Validate with `rungated validate file.yaml`. The reference is in docs/manifest.md.
