# Load test, one machine, gateway in Docker on cores 0-1 (2 cpus, 1g), k6 on cores 2-15

Gateway: {"go":"go1.24.13","version":"0.10.0"} at http://127.0.0.1:18300. Duration per step: 30s.

| Target rps | Achieved rps | HTTP p95 ms | HTTP p99 ms | Decision p95 ms | Errors % |
| --- | --- | --- | --- | --- | --- |
| 250 | 250 | 1.01 | 1.19 | 0.43 | 0.00 |
| 500 | 500 | 0.81 | 1.13 | 0.36 | 0.00 |
| 1000 | 1000 | 0.81 | 2.21 | 0.36 | 0.00 |
| 2000 | 1998 | 2.15 | 23.42 | 0.35 | 0.00 |

Client machine:

```
host:   fedora
cpu:    AMD Ryzen 7 4800H with Radeon Graphics x 14
memory: 15 GB
kernel: 7.2.8-200.fc44.x86_64
```

Add the server machine details from the server terminal output.
