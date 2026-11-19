# Blackboard

An agent-first local registry for shared topics, sessions, issues, merge requests
and worktrees. Topics describe the goal; links describe who is working where.
Tern tabs are optional context, not the source of truth.

## Start

Install the released CLI (macOS or Linux with Homebrew):

```sh
brew install beeemT/tap/blackboard
blackboard help
```

Or build from source with Go 1.27.4+: `go install ./cmd/blackboard` (uses `GOBIN`).
The CLI creates `~/.blackboard/db` automatically. Use `--db PATH` before the
command for an isolated database; existing databases are not moved automatically.

```sh
blackboard schema
blackboard find billing
blackboard status
printf '%s\n' '{"operation":"topic.create","operation_id":"billing-create-1","actor":"omp:session-123","id":"billing","title":"Billing","goal":"Ship invoice reconciliation","priority":3}' | blackboard apply
```

All input/output is JSON. Errors go to stderr with exit code 1. Writes require
`actor` and `operation_id`; retry with the same ID and identical payload.
`schema` lists operations and required fields. Topics close explicitly with an
outcome. Polling and heartbeats do not count as progress. Priority stays fixed;
attention reflects status and time since meaningful work.

## Agent setup

Install the CLI above, then expose its bundled [Blackboard skill](skills/blackboard/SKILL.md)
to agents that discover `~/.agents/skills`. The link follows Homebrew upgrades:

```sh
mkdir -p "$HOME/.agents/skills"
skill="$HOME/.agents/skills/blackboard"
# Stop if a skill is already installed; preserve it and reconcile manually.
if [ ! -e "$skill" ] && [ ! -L "$skill" ]; then
  ln -s "$(brew --prefix blackboard)/share/blackboard/skills/blackboard" "$skill"
else
  printf '%s\n' "Blackboard skill already exists; left unchanged."
fi
```

For a source checkout, copy `skills/blackboard/` to the same location instead,
without overwriting an existing skill. Other agents may need their own skill
search path configured; installing the CLI does not configure agents automatically.

Add this Blackboard-only directive to your `~/.agents/AGENTS.md` (preserve its
existing instructions). If your agent does not load that file, put the directive
in the instruction file it actually reads, such as the project's `AGENTS.md`:

```markdown
- Use the [blackboard skill](skills/blackboard/SKILL.md) when starting or joining a work topic, at meaningful milestones, and at handoffs. Keep topic, session, and work links current; close topics only when their goals are met.
```

The relative link above is for `~/.agents/AGENTS.md`. For a project instruction
file, use the installed skill's path or its
[public source link](https://github.com/beeemT/blackboard/blob/main/skills/blackboard/SKILL.md).
Restart or reload the agent's skill discovery, then ask it to read the skill and
run `blackboard status`. Setup does not need to create a topic or modify a database.

## Reconcile

Git collects local worktree facts without fetch or changes. GitLab uses an
already-authenticated `glab` to read MR and current-head pipeline state.
Sources must be selected explicitly in a JSON config:

```json
{"sources":[
  {"name":"worktree","kind":"git","path":"/absolute/path/to/worktree"},
  {"name":"mr","kind":"gitlab","host":"gitlab.example.com","project":"group/project","mr":123}
]}
```

```sh
blackboard reconcile --config sources.json
blackboard reconcile --config sources.json --watch --interval 5m
```

Watch is opt-in, not an installed service. Failed sources retain their last known
facts and report separate health. Observations never close topics or authorize
agent actions. Link work using absolute worktree paths or canonical MR URLs.

## Development

```sh
mise run check    # vet + race tests
mise run smoke    # build + isolated CLI/Git smoke
mise run install  # install in GOBIN
```

Architecture: domain → services → repository interfaces, with SQLite persistence,
read-only reconciliation adapters and a JSON CLI. See [design](docs/design.md).
No automatic Tern/OMP discovery, agent steering, cloud sync or dashboard yet.

## Releases

After merging to the default branch, an owner can push a stable `vMAJOR.MINOR.PATCH`
tag. CI tests the tagged code, builds macOS/Linux arm64/amd64 archives, publishes
a GitHub release with checksums, and updates `beeemT/tap` with the existing
`HOMEBREW_TAP_TOKEN` secret. Publishing uses the `release` environment; any configured
reviewer gate must approve it. CI and build jobs have read-only repository access.
Older tags never downgrade an existing tap formula. Releases queue rather than replace
waiting runs (up to GitHub's 100-run limit). Archive metadata is deterministic; a retry
compares existing published assets and refuses any byte changes. It never overwrites
release assets. If a rebuilt asset differs, use a new version tag rather than replace it.

Install a release with `brew install beeemT/tap/blackboard`. The
formula includes the skill under `$(brew --prefix blackboard)/share/blackboard/skills`;
it does not change your agent configuration. Git and `glab` remain optional collector tools.

To test packaging without publishing, run `python3 scripts/release.py v0.0.0` with
an empty `dist/` directory. It builds all four targets, checksums and the formula.
No tag, release or tap update is created by that command.
