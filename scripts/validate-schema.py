#!/usr/bin/env python3
import copy
import glob
import json
import sys
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parent.parent
schema = json.loads((ROOT / "spec/ats/manifest.v0.schema.json").read_text())
Draft202012Validator.check_schema(schema)
validator = Draft202012Validator(schema)

paths = []
for pattern in ("spec/ats/examples/*.yaml", "deploy/compose/manifests/*.yaml", "internal/bench/agent.yaml", "deploy/helm/rungated/manifests/*.yaml"):
    paths += sorted(glob.glob(str(ROOT / pattern)))

failed = 0
for p in paths:
    doc = yaml.safe_load(Path(p).read_text())
    errors = sorted(validator.iter_errors(doc), key=lambda e: list(e.path))
    if errors:
        failed += 1
        print("FAIL", p)
        for e in errors:
            print("  ", "/".join(str(x) for x in e.path), e.message)
    else:
        print("ok  ", Path(p).relative_to(ROOT))

if not paths:
    print("no manifests found")
    sys.exit(1)

base = yaml.safe_load(Path(paths[0]).read_text())
mutations = {
    "unknown field": lambda d: d["spec"].update({"bogus": 1}),
    "bad risk tier": lambda d: d["spec"].update({"riskTier": "extreme"}),
    "bad mode": lambda d: d["spec"]["autonomy"].update({"default": "yolo"}),
    "empty model list": lambda d: d["spec"]["model"].update({"allowed": []}),
    "bad id": lambda d: d["metadata"].update({"id": "Bad ID"}),
    "threshold out of range": lambda d: d["spec"].setdefault("risk", {}).setdefault("thresholds", {}).update({"warn": 500}),
    "bad fail mode": lambda d: d["spec"].setdefault("risk", {}).update({"failMode": "maybe"}),
    "missing owner": lambda d: d["metadata"].pop("owner"),
}
for name, mutate in mutations.items():
    doc = copy.deepcopy(base)
    mutate(doc)
    if not list(validator.iter_errors(doc)):
        failed += 1
        print("FAIL schema accepted an invalid manifest:", name)
    else:
        print("ok   rejects:", name)

sys.exit(1 if failed else 0)
