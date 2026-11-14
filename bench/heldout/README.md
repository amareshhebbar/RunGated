# Held out set

The generated corpus in `bench/corpus.jsonl` was written next to the detectors, so it measures regressions, not real world coverage. Detection claims need data the rules were never tuned on. This directory is where that data goes.

## Rules

1. Cases here are never used to write or tune rules, thresholds or weights.
2. Every case comes from a source outside this repository: a pilot team's real traffic, a public dataset, a red team session run by someone who has not read the rules.
3. Every case is labeled by a person, attack or benign, before it is run.
4. Benign cases must be real. Teams care about false positives more than recall, so at least half of any held out file should be benign.
5. After a run, misses may be studied. Fixing a miss moves that case to `bench/corpus.jsonl` and it is replaced here with a fresh one.

## Format

One JSON object per line, the same format as `bench/corpus.jsonl`. `template.jsonl` has one example of each kind. Fields: `id`, `class`, `label` (attack or benign), `kind` (llm.request, llm.response, tool.call, tool.result, mcp.resource, egress.request, egress.response), `parts` (source and text), and for tool calls `tool`, `server`, `args`, for egress `host`, `url`, `method`.

## Run

    rungated bench run --corpus bench/heldout/pilot.jsonl --md heldout-report.md
    rungated bench run --corpus bench/heldout/pilot.jsonl --config tier1.yaml --md heldout-tier1.md

Report recall and false positive rate with the case count and the source, for example "recall 81 percent on 140 attacks from team X's traffic, false positives 1.2 percent on 600 benign actions". Never report the generated corpus number as a detection claim.

## Privacy

Redact customer data before it lands in this directory. Anything committed here should be safe to share with an auditor.
