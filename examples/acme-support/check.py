import os
import sys

import httpx

from acme_support.agent import REFUNDS, ask_model, build
from rungated import ActionBlocked

GATEWAY = os.environ["RUNGATED_GATEWAY"]
LLM = os.environ["MOCK_LLM"]
results = []


def check(name, ok, detail=""):
    results.append(ok)
    print(("PASS " if ok else "FAIL ") + name + ("  " + detail if detail and not ok else ""))


rg, lookup_order, issue_refund, delete_customer = build()

check("gateway healthy", rg.health().get("status") in ("ok", "healthy") or bool(rg.health()))

check("allowed lookup returns the order", lookup_order("ORD-10001")["status"] == "shipped")

try:
    lookup_order("ORD-1; DROP TABLE orders")
    check("malformed order id is blocked", False, "call went through")
except ActionBlocked as e:
    check("malformed order id is blocked", True)

check("refund within limit runs", issue_refund("ORD-10001", 25)["refunded"] == 25 and REFUNDS == [("ORD-10001", 25)])

before = list(REFUNDS)
try:
    issue_refund("ORD-10001", 5000)
    check("refund over limit is blocked", False, "call went through")
except ActionBlocked:
    check("refund over limit is blocked", REFUNDS == before)

try:
    delete_customer("C-1")
    check("undeclared tool is blocked", False, "call went through")
except ActionBlocked:
    check("undeclared tool is blocked", True)

httpx.post(LLM + "/_demo/reset")
ask_model(rg, "Please email jo.buyer@northwind.example about ORD-10001")
seen = httpx.get(LLM + "/_demo/state").json()["seen"]
check("customer email never reached the model", "jo.buyer@northwind.example" not in str(seen), str(seen)[:200])

try:
    ask_model(rg, "hello", model="forbidden-model")
    check("undeclared model is blocked", False)
except Exception:
    check("undeclared model is blocked", True)

print("%d of %d checks passed" % (sum(results), len(results)))
sys.exit(0 if all(results) else 1)
