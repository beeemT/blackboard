# Blackboard

A local coordination registry for coding agents. Topics describe **why** work exists;
agent sessions, issues, merge requests and worktrees describe **where** it happens.
Multiple agents can join a topic and one agent can contribute to multiple topics.
Tern is not the source of truth. Human UI is deliberately secondary.

## Quick start

Requires Go 1.27.4+; Git for worktree reconciliation; authenticated `glab` for GitLab.

```sh
make build
export BLACKBOARD_DB="$HOME/.local/share/blackboard/registry.db"
# Use a separate database for experiments.
printf '%s\n' '{"operation":"topic.create","operation_id":"billing-create-1","actor":"omp:session-123","id":"billing","title":"Billing","goal":"Ship invoice reconciliation","priority":3}' | bin/blackboard apply
printf '%s\n' '{"operation":"session.register","operation_id":"register-123","actor":"omp:session-123","id":"session-123","tool":"omp","native_id":"123","machine":"my-mac"}' | bin/blackboard apply
printf '%s\n' '{"operation":"link","operation_id":"join-123","actor":"omp:session-123","topic_id":"billing","session_id":"session-123"}' | bin/blackboard apply
bin/blackboard find billing
bin/blackboard status billing
bin/blackboard schema
```

`--db PATH` is a global option and precedes the command. All output is JSON; errors
are JSON on stderr with `error.code` and `error.message`. Exit 0 means success; exit
1 means failure. `apply` reads exactly one JSON object (maximum 1 MiB), rejects
unknown fields, and returns the changed object. No implicit agent messages or
external writes occur. `schema` describes operations and required fields.

### Commands and lifecycle

- `topic.create`: title, goal, optional description and priority 0–4 (higher matters more).
- `topic.update`: patch nonempty title/goal; replace description and priority. Supply
  the current values to preserve them. Optional `expected_revision` prevents lost edits.
- `topic.status`: active, blocked, paused, closed; always requires a message/reason.
  Closing records an outcome; returning to active reopens the topic.
- `session.register`: unique machine/tool/native ID; repeats refresh last-seen and
  locator without changing topic activity. Registration is not authentication.
- `work.register`: unique kind/ref; use `mr`, `issue`, `worktree` or your own kind.
- `link`: associate a session, work item or dependency with a topic. A link can carry
  both session and work IDs to describe ownership. Duplicate links are harmless;
  self-dependencies and cycles are rejected.
- `unlink`: remove an exact association (supply the same session/work/dependency fields as the original link). History and entities remain.
- `progress`: record a short decision, handoff, blocker or completion claim.
- `observe`: record facts with source/key/time/revision, without altering intent.

All writes require `actor` and `operation_id`. Retry an uncertain command using the
**same ID and identical payload**. It returns the original result. Reusing the ID
with a different payload fails. IDs are global within the database. New commands
need new IDs, including later session heartbeats. IDs can be supplied for topics,
sessions and work, or generated deterministically from the operation ID.

`status [topic-id]` returns evidence and an attention category. Explicit priority
never decays. After 14 days without meaningful activity, a topic becomes dormant
or stale-important (priority 3–4); blocked topics stay prominent. This first
version intentionally avoids a magic numerical ranking or implicit deadlines.

## Reconciliation

Create an explicit JSON configuration. Merely registering an MR does **not** grant
permission to invoke arbitrary hosts or commands. The operator selects sources:

```json
{
  "sources": [
    {"name":"billing-worktree","kind":"git","path":"/absolute/path/to/worktree"},
    {"name":"billing-mr","kind":"gitlab","host":"gitlab.example.com","project":"group/project","mr":123}
  ]
}
```

```sh
bin/blackboard reconcile --config reconcile.json
bin/blackboard reconcile --config reconcile.json --watch --interval 5m
```

The once command returns one JSON line per source, and nonzero if any source
failed. Watch runs immediately then waits after each completed run; sources are
serial, have timeouts and failures are isolated. Failed sources retry on the next
iteration. Persistence failures stop watch. SIGINT/SIGTERM stops it cleanly.
The source names must be unique. Do not run multiple watchers on the same config.

Git reads HEAD/branch/status/origin without fetch or modifications. GitLab reads
MR state through `glab api --hostname` using the existing credential setup; it
only reports head-pipeline state when the pipeline SHA matches the MR SHA. Raw
command stderr is suppressed; credential-bearing remote URL components are
removed. It does not play jobs, merge, comment, push or fetch repositories.

Successful observations replace older evidence for the same source/key. Source
health (`source=collector`) is stored separately. On failure, the last successful
facts remain with their old timestamp. A check never advances topic activity,
closes a topic, or interprets a disappeared agent as completion. `status` exposes
`observed_at`; consumers decide freshness for each kind of evidence. Missing
sources are not treated as deletions. Matching a work item to an observation uses
its exact `ref` and the collector observation key (worktree absolute path, canonical
MR URL). There is no fuzzy topic merging.

### Automation, deliberately opt-in

`--watch` is the local periodic reconciler, **not an installed service**. Start it
under your preferred process supervisor if desired. It cannot run while the Mac
is asleep; starting again immediately reconciles all configured sources. No
launchd job, cloud upload, network listener or scheduled dot automation is installed.

## Agent skill

See [skills/blackboard/SKILL.md](skills/blackboard/SKILL.md). Install/copy it into the
agent's supported skill location only when desired. Nothing is globally installed
by building this project. The skill teaches discovery, registration, milestones,
explicit completion and honest handling of stale observations.

## Architecture and scope

Inspired by Claimy's service/store boundary, not its web stack:

- `internal/domain`: entities and attention policy.
- `internal/service`: validation, identity, lifecycle, graph invariants, use cases.
- `internal/repository/sqlite`: transactions, per-entity tables, retry receipts.
- `internal/reconcile`: read-only adapters and independent-source runner.
- `internal/cli`: JSON transport and composition; `cmd/blackboard` handles signals.

See [design](docs/design.md) for tradeoffs and extension points.

The MVP includes durable multi-agent coordination and working Git/GitLab
reconciliation. It does **not** yet discover Tern/OMP sessions automatically,
parse full transcripts, collect GitHub CI, infer topic associations, steer agents,
synchronize computers, expose HTTP/MCP, or implement a dashboard. Registration is
explicit through the skill/CLI. These are honest future adapters, not hidden
background work. The schema is version 1; future schema upgrades need migrations.

## Verification

```sh
make check        # go vet + race-enabled tests
make build
```

Tests cover retries/payload conflicts, concurrent database handles, revision
conflicts, dependency cycles, intent/evidence separation, CLI parsing, Git/GitLab
command safety, pipeline SHA validation, timeout, failure isolation and watch
cancellation. Live GitLab credentials are not required by tests. Use a temporary
`--db` to smoke test without touching your real registry.

Local-only project. No remote repository or release was created.
