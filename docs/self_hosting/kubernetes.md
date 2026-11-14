# Kubernetes

```bash
helm install rungated deploy/helm/rungated \
  --set image.tag=0.10.0
```

The chart runs as a non root user with a read only root filesystem. Values are documented in `deploy/helm/rungated/values.yaml`.

For production, run at least two replicas with Postgres. Add NATS JetStream, in Enterprise, so a single pod failure cannot lose an audit record. See [Operations](operations.md).
