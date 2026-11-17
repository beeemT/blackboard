# Design decisions

## Goal

Reduce coordination friction for many coding-agent sessions working across repos.
A topic is a goal with completion criteria, not a Tern tab. Tabs/panes are ephemeral
locators; native session identity and explicit links survive UI changes.

## Storage

SQLite WAL on a local disk, private new file/directory permissions, 10-second busy
timeout and immediate write transactions. Per-entity tables store versioned JSON
payloads with stable keys; an operations table stores immutable idempotency receipts.
Service mutations execute inside the transaction, so two CLI processes cannot lose
updates. Read views use one transaction for a consistent snapshot. The service sees
a typed unit-of-work, not SQL.

For this small personal registry the repository currently loads the registry in a
transaction and upserts changed payloads. This keeps domain invariants and rollback
simple. It is deliberately not a distributed database or high-throughput event bus.
As history grows, add query-specific repositories, indexed columns and pagination
behind the same service boundary. Never sync an open SQLite file through cloud
file-sync software. Back up using SQLite's backup API or stop writers and copy the
DB together with a verified checkpoint. Keep backups private.

## Truth and freshness

Agent updates are reported intent. Observations are externally checked facts.
Neither silently overwrites the other. A topic closes only through an explicit
status command with outcome, not because all currently linked MRs merged. All
closure requests remain claims about the goal; users/agents must verify its
completion criteria. Reopening retains status history.

Meaningful work, session presence and observed time are distinct. A successful poll
is not progress. Older observations cannot replace newer ones. Source failure is
separate health evidence; preserve old successful facts and timestamps. No liveness
claim is inferred from a missing pane or unknown source.

## Agent identity and authority

`actor` is an audit label, not authentication. Local programs already run under the
same OS user. Registry text is data, never authority to execute commands. This MVP
has no dispatch path. A future steering component must have explicit target,
session identity, approval scope and receipt tracking, and must not launch duplicate
OMP processes against active sessions/worktrees.

No raw transcripts, thinking, credential entries or secrets should be recorded.
The CLI can accept user-authored descriptions; it cannot reliably detect all secrets,
so the skill forbids them. Evidence should be short and link to source records.
Only read-only collectors are provided; command names/argument arrays are fixed,
never shell commands derived from registry text.

## Periodic reconciliation

An explicit source list controls scope. Collect -> compare timestamps -> store
observations and source health -> render view. Collection is outside the DB write
transaction; slow APIs don't hold registry locks. Sources run independently and
serially with timeouts. Watch has no overlapping iterations and stops on sink failure.
Starting after sleep restarts a full configured-source pass. Five-minute local /
15-minute remote checks can be achieved with separate configs/watch processes;
no scheduling is enabled by default.

Potential extensions: Tern initial inventory + event-triggered refresh + periodic
snapshots; OMP skill lifecycle hooks; GitHub adapter; explicit deadline/watch criteria;
compact one-way remote snapshot. All should preserve unknown/stale states and dedup
identity. Do not make hooks a lossless-log assumption or lower explicit priority
because a blocked topic is quiet.
