# Self-Hosted Arbiter

Arbiter is plausible to self-host today if you keep the deployment shape honest.

The recommended profile is:

- one deployment per team, environment, or trust boundary
- persistent disk for bundle and override state
- bearer auth on the gRPC surface
- TLS at the ingress/service-mesh layer or directly in the process
- one replica or sticky routing when expert sessions are in play

This is not a hosted multi-tenant control plane. If you need one shared service for many customers, tenant isolation, browser auth, durable project state, or managed runners, that is what `arbiter-cloud` is for.

## What works well

- stateless rule evaluation
- feature flags
- strategy evaluation
- continuous arbiters, especially through `arbiter-runtime` with bounded source and delivery parallelism
- local agent deployments that keep evaluation close to the caller

Those paths are easy to run behind a load balancer because the evaluator can stay stateless between requests.

For continuous arbiters, the best current scale-up path is internal I/O parallelism in the runner: different external sources can be polled concurrently and different handler targets can drain concurrently, while one target or worker still keeps ordered delivery.

The scaling model today is:

- stateless eval, flags, and strategies scale out behind a load balancer
- expert sessions scale up on one instance but still need sticky routing or a single replica per session-bearing path
- continuous arbiters scale up inside `arbiter-runtime` with bounded source/delivery parallelism and scale out by splitting bundles or arbiter graphs across runners

## What needs care

- expert sessions stay in-process and do not migrate between replicas
- bundles and overrides must be persisted to disk if you want restart durability
- the engine still uses one flat namespace, so multi-customer self-hosting means separate deployments or an external tenancy wrapper

For expert sessions, the safe operating rule is simple: use one replica or sticky routing for the session-bearing path.

## Kubernetes reference profile

The repo ships a single-team reference manifest in [`deploy/k8s.yaml`](../deploy/k8s.yaml). It assumes:

- one `PersistentVolumeClaim` mounted at `/var/lib/arbiter`
- one `Secret` named `arbiter-auth` with a `token` entry
- a `ClusterIP` service on port `8081`
- an image reference you control in your own registry

Create the auth secret first:

```bash
kubectl create namespace arbiter-system --dry-run=client -o yaml | kubectl apply -f -
kubectl -n arbiter-system create secret generic arbiter-auth \
  --from-literal=token='replace-me'
```

Then apply the manifest:

```bash
kubectl -n arbiter-system apply -f deploy/k8s.yaml
kubectl rollout status deploy/arbiter -n arbiter-system
```

The reference deployment enables:

- file-backed state with `--data-dir /var/lib/arbiter`
- bearer auth with `--auth-token-file /var/run/secrets/arbiter/token`
- gRPC message bounds with `--max-recv-bytes` and `--max-send-bytes`
- per-caller rate limits
- per-owner expert-session caps

Apply the same rule to `arbiter-runtime`: protect `--grpc` with `--auth-token` / `--auth-token-file`, and use `--tls-cert`, `--tls-key`, and optionally `--tls-client-ca` when the runtime control RPC leaves localhost.

If `arbiter-runtime` talks to a remote capability plugin, make that transport explicit too: prefer `grpcs://...` plus `--capability-token`, `--capability-ca-file`, and `--capability-server-name` over ambient network trust.

Do not treat that posture as hidden configuration. Check `/status`, `RuntimeService.GetRuntimeCapabilities`, or `RuntimeService.GetRuntimeStatus` and verify the runtime is actually reporting the `readiness`, `transport`, `capabilities`, and `activity` shape you intended, including auth/TLS/public-listener and capability-transport posture.

## Container defaults

[`deploy/Dockerfile`](../deploy/Dockerfile) now defaults to:

- `arbiter serve --grpc 0.0.0.0:8081 --data-dir /var/lib/arbiter`
- a stable non-root UID/GID
- `/var/lib/arbiter` as the writable state path

That means a plain container run is no longer an obviously ephemeral demo:

```bash
docker run --rm \
  -p 8081:8081 \
  -v arbiter-data:/var/lib/arbiter \
  arbiter:latest
```

Add auth and TLS before exposing it beyond a private network.

## Edge and agent patterns

Two patterns are credible in production:

1. One central Arbiter deployment behind ingress for stateless eval, flags, and bundle lifecycle.
2. A central control plane plus `arbiter-agent` sidecars near callers when you want local evaluation and upstream bundle sync.

The agent path is the better story when you want local low-latency eval without turning the engine into a shared multi-tenant service.

Treat the agent with the same discipline as the runtime: inspect `/status` or `AgentService.GetAgentStatus` and verify the `readiness`, `transport`, and `sync` sections, including local listener posture, upstream auth/TLS posture, readiness reason, and bundle/override watch connectivity, instead of assuming the sidecar is healthy because the process is up.

If the agent's local gRPC surface is reachable beyond localhost, harden it the same way: `--auth-token` / `--auth-token-file`, plus `--tls-cert`, `--tls-key`, and optionally `--tls-client-ca`.

## Decision rule

Self-host `arbiter` when you own one trust boundary and want a governed decision engine.

Use `arbiter-cloud` when you want the managed surface around that engine: tenancy, auth flows, project state, hosted runners, dead letters, billing, and operator workflow.
