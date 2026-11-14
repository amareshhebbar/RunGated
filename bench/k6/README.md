# k6 load test

Drives POST /v1/decide with the bench corpus at a constant arrival rate and checks latency, attack recall and false positives under load.

```
rungated serve --dev --data /tmp/rungated-load &
k6 run bench/k6/decide.js
RATE=2000 DURATION=120s MAX_VUS=800 k6 run bench/k6/decide.js
```

Variables: BASE_URL, AGENT, API_KEY, RATE, DURATION, MAX_VUS. The script registers internal/bench/agent.yaml in setup, so a fresh gateway needs nothing else. Thresholds: p95 under 50 ms end to end, decision p95 under 25 ms, recall above 90 percent, false positives under 2 percent.
