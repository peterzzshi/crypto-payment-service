# Kubernetes Manifests

Illustrative deployment topology for crypto-payment-service. Not applied to any cluster — demonstrates production-grade patterns.

The manifests are design artifacts, not a deployable release: image names are placeholders, health routes are absent, and the worker currently ignores the ConfigMap's poll interval and batch size.

## Components

- **webhook-server-deployment.yaml** - HTTP server for BitGo webhooks (3 replicas)
- **worker-deployment.yaml** - Background worker for withdrawal processing (2 replicas)
- **service.yaml** - LoadBalancer exposing webhook-server on port 80
- **configmap.yaml** - Non-sensitive configuration (log level, poll interval, batch size)
- **secret.yaml** - Credentials with GCP Secret Manager CSI integration notes
- **hpa.yaml** - Horizontal Pod Autoscalers for both deployments
- **pdb.yaml** - Pod Disruption Budgets to ensure availability during maintenance

## Deployment Topology

```
                    ┌─────────────────┐
                    │  LoadBalancer   │
                    │   (port 80)     │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │ webhook-server  │
                    │   (3-10 pods)   │
                    └────────┬────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
     ┌────────▼────────┐         ┌─────────▼─────────┐
     │   PostgreSQL    │         │  withdrawal-worker │
     │    (managed)    │◄────────│    (2-8 pods)      │
     └─────────────────┘         └────────────────────┘
```

- **Webhook Server**: Stateless, scales based on CPU/memory, accepts BitGo webhooks
- **Withdrawal Worker**: Polls for APPROVED withdrawals and exercises broadcast/confirmation flow through the demo's stub broadcaster
- **PostgreSQL**: Managed service (Cloud SQL, RDS, etc.) with connection pooling

## Concurrency Safety

Multiple workers claim APPROVED withdrawals using `SELECT ... FOR UPDATE SKIP LOCKED` and atomically mark them BROADCASTING before the external call. This prevents concurrent workers from selecting the same row, but it does not by itself guarantee exactly-once external broadcast across process crashes or ambiguous provider timeouts; production integration would also need a provider idempotency/reconciliation strategy.

## Secret Management

In production, replace `secret.yaml` with GCP Secret Manager CSI Driver:

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: crypto-payment-secrets-provider
spec:
  provider: gcp
  parameters:
    secrets: |
      - resourceName: "projects/PROJECT_ID/secrets/database-url/versions/latest"
        path: "database-url"
```

Mount as volume in deployment:

```yaml
volumes:
- name: secrets-store
  csi:
    driver: secrets-store.csi.k8s.io
    readOnly: true
    volumeAttributes:
      secretProviderClass: crypto-payment-secrets-provider
volumeMounts:
- name: secrets-store
  mountPath: "/mnt/secrets"
  readOnly: true
```

## Apply Order

```bash
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f webhook-server-deployment.yaml
kubectl apply -f worker-deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f hpa.yaml
kubectl apply -f pdb.yaml
```

## Health Checks

- **Webhook Server**: The manifests reference `/health` and `/ready`, but the server does not currently register those routes. The probes will fail until the endpoints are implemented or the manifests are changed.
- **Worker**: No HTTP endpoints (logs track progress)
