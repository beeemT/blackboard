#!/usr/bin/env python3
"""Exercise the built CLI with isolated temporary registry and Git fixture."""
import json, pathlib, subprocess, tempfile
binary = pathlib.Path(__file__).resolve().parents[1] / "bin" / "blackboard"
with tempfile.TemporaryDirectory(prefix="blackboard-smoke-") as tmp:
    tmp = pathlib.Path(tmp)
    db = tmp / "registry.db"
    def run(*args, payload=None, ok=True):
        result = subprocess.run([str(binary), "--db", str(db), *args], input=json.dumps(payload) if payload else None, text=True, capture_output=True)
        assert (result.returncode == 0) == ok, (result.stdout, result.stderr)
        return [json.loads(line) for line in result.stdout.splitlines()]
    create = dict(operation="topic.create", operation_id="create", actor="smoke", id="demo", title="Demo", goal="Verify registry", priority=4)
    assert run("apply", payload=create) == run("apply", payload=create)
    repo = tmp / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "main", str(repo)], check=True, capture_output=True)
    subprocess.run(["git", "-C", str(repo), "-c", "user.name=Smoke", "-c", "user.email=smoke@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture"], check=True, capture_output=True)
    config = tmp / "sources.json"
    config.write_text(json.dumps(dict(sources=[dict(name="fixture", kind="git", path=str(repo))])))
    before = run("status")[0]["registry"]["topics"][0]
    observations = run("reconcile", "--config", str(config))
    assert observations[0]["observations"][0]["state"] == "clean"
    after = run("status")[0]["registry"]["topics"][0]
    assert before == after, "polling changed reported intent"
    run("apply", payload=dict(operation="topic.status", operation_id="close", actor="smoke", topic_id="demo", status="closed", message="Smoke passed"))
    assert run("status", "demo")[0]["registry"]["topics"][0]["status"] == "closed"
print("Blackboard CLI smoke passed")
