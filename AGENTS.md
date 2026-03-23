# Arbiter Agent Instructions

## gts / Orchard Safety

- Do not run raw `gts` commands in this repo. Use [`scripts/gts-safe`](/home/draco/work/arbiter/scripts/gts-safe) instead.
- Do not run more than one `gts` process at a time. The wrapper enforces a repo lock for this.
- Prefer cached reads, but do not default to `scripts/gts-safe index .` on this repo. Build a cache only for a narrowly scoped directory when it is genuinely needed, then let the wrapper inject `--cache .gts/index.json` for follow-up reads.
- Scope structural analysis to the narrowest path that answers the question. Do not point `gts` at the whole repo unless that scope is actually required.
- Do not launch background `gts` jobs, `gts mcp`, or `gts index --watch` unless the user explicitly asks for a long-lived session. The wrapper blocks those modes unless `GTS_ALLOW_LONG_RUNNING=1` is set.
- Do not use `gts refactor --write` or `gts mcp --allow-writes` unless the user explicitly asked for a write operation. The wrapper blocks those modes unless `GTS_ALLOW_WRITES=1` is set.
- Default safety budget for `scripts/gts-safe`: `GOMAXPROCS=2`, `GOMEMLIMIT=2GiB`, `GOGC=50`, and a 120s timeout for one-shot commands. Override only with clear need.
- Full-repo indexing is currently treated as exceptional work. On this repo, a root `gts index .` exceeded the default 120s budget and peaked around 2.4 GiB RSS, so prefer direct code reads unless a scoped `gts` query is clearly the better tradeoff.

## General Workflow

- For repo analysis, mix targeted code reads with a small number of `scripts/gts-safe` queries rather than fan-out background investigations.
- Before heavy structural analysis, check whether `.gts/index.json` already exists and reuse it when possible.
- If a `gts` command times out or hits the repo lock, narrow the scope instead of retrying in parallel.
