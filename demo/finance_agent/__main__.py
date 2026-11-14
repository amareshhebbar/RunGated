import argparse
import json

from .agent import FinanceAgent


def main() -> None:
    p = argparse.ArgumentParser(prog="finance_agent")
    p.add_argument("prompt")
    p.add_argument("--unprotected", action="store_true")
    p.add_argument("--json", action="store_true")
    a = p.parse_args()
    agent = FinanceAgent(protected=not a.unprotected)
    try:
        res = agent.run(a.prompt)
    finally:
        agent.close()
    if a.json:
        print(json.dumps({"final": res.final, "events": res.events, "error": res.error, "session": res.session}, indent=2))
        return
    for e in res.events:
        print("tool %s %s -> %s%s" % (e["tool"], json.dumps(e["args"])[:120], e["result"][:120].replace("\n", " "), " [error]" if e["is_error"] else ""))
    if res.error:
        print("error:", res.error)
    print("final:", res.final)


main()
