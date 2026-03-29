# Status Issue Codes

Arbiter status surfaces expose a canonical `issues` list across runtime, agent, and hosted control.

The same vocabulary is available in product surfaces too:

- `arbiter status-issues` for local inspection
- `arbiter status-issues grpcs://host:port --surface runtime|agent|control` for live CLI inspection against a remote surface
- `GetStatusIssueCatalog` on runtime, agent, and control gRPC services for machine-readable clients
- `GET /status/issues` on runtime, agent, and hosted control HTTP status listeners for scoped JSON catalogs

Every catalog and status surface also advertises:

- `operator.product` — the product name (`arbiter`)
- `operator.build_version` — the running build version
- `operator.operator_contract_version` — the operator-surface contract version

Each issue has:

- `severity` — `warning` or `error`
- `scope` — the subsystem family, such as `transport`, `sync`, or `audit`
- `subject` — the concrete target being described
- `code` — the stable machine-facing identifier
- `message` — the current human-facing detail
- `blocking` — whether the issue should count as a readiness-blocking problem

The `code` field is the contract. Treat `message` as explanatory text, not a stable parsing surface.
The catalog also records which surface each code belongs to: `runtime`, `agent`, `control`, or a shared subset such as the common `public_control_insecure` transport warning.

## Readiness

| Code | Blocking | Meaning |
|------|----------|---------|
| `status_unavailable` | yes | Status payload is unavailable |
| `first_tick_incomplete` | yes | Runtime has not completed its first tick |
| `initial_sync_incomplete` | yes | Agent has not completed its initial sync |
| `not_ready` | yes | Another readiness-blocking condition |

## Transport

| Code | Blocking | Meaning |
|------|----------|---------|
| `public_control_insecure` | no | Public control listener has no TLS or auth |
| `capability_transport_insecure` | no | Runtime capability transport has no TLS or auth |
| `upstream_transport_insecure` | no | Agent upstream transport has no TLS or auth |

## Runtime Data Path

| Code | Blocking | Meaning |
|------|----------|---------|
| `source_unavailable` | no | Runtime source is unavailable |
| `source_failures` | no | Runtime source has consecutive failures |
| `sink_unavailable` | no | Runtime sink is unavailable |
| `sink_failures` | no | Runtime sink has consecutive failures |
| `sink_ambiguous` | no | Runtime sink has ambiguous deliveries |

## Agent Sync

| Code | Blocking | Meaning |
|------|----------|---------|
| `upstream_error` | no | Agent observed an upstream control-plane error |
| `bundle_never_synced` | yes | Bundle has never synced |
| `bundle_stale` | yes | Bundle sync is stale |
| `override_stale` | yes | Override sync is stale |
| `bundle_watch_disconnected` | no | Bundle watch is disconnected |
| `override_watch_disconnected` | no | Override watch is disconnected |
| `bundle_sync_error` | no | Bundle sync has a recorded error |
| `override_sync_error` | no | Override sync has a recorded error |

## Hosted Control Durability

| Code | Blocking | Meaning |
|------|----------|---------|
| `bundle_persistence_unhealthy` | yes | Bundle persistence is unhealthy |
| `override_persistence_unhealthy` | yes | Override persistence is unhealthy |
| `audit_unhealthy` | yes | Audit recording is unhealthy |
