# TASK-43 server-side files API evidence

Captured 2026-08-28 from branch `files-api` using a freshly built worktree
binary on test-only port 4493.

## Automated verification

- `go test -count=1 ./...` — PASS
- `go vet ./...` — PASS
- `go test -race -count=1 ./internal/fileapi ./internal/fileindex ./internal/fileresolver ./internal/fileroots ./internal/repoctx ./internal/servecmd` — PASS
- every `tools/herder/tests/check-*.sh` gate with `HERDER_BIN` unset — PASS
  - `check-web-serve.sh`: `PASS=23 FAIL=0`
  - `check-live-contract.sh`: `PASS=10 FAIL=0 SKIP=0`
  - `check-serve-watch.sh`: `PASS=6 FAIL=0`
  - the serve-watch cleanup printed existing permission warnings for its
    root-owned temporary module cache after all six assertions passed; the
    gate itself exited zero and the full loop continued to completion.

Focused real-shape tests cover ordinary nested roots, linked worktrees,
complete/degraded/failed indexes, a healthy+failed mixed endpoint response,
text/binary/soft-cap/hard-cap reads, tree listings, `.git` refusal, and symlink
escape refusal. Review follow-up also pins missing-Git and forced-timeout cases:
both retain `cwd`, omit the optional `git` object, and return without error so
fleet, SSE, and agent detail availability never depends on Git health.

Code review: harness-native fallback — the portable review's required agent
dispatch was prohibited by the repository instructions. The sequential review
found one medium optional-enrichment availability issue; the conductor chose
to fix it, the fix above was applied, and no actionable findings remain.

## Live server evidence

SSE hello from the tested process:

```json
{"buildIdentity":"executable:ec0ec470dbd420d081c1434911bc3efe9288ee326135d7164f13efa52b8fdb5f"}
```

`GET /api/resolve?q=files-resolution-design.md&agent=impl-mevi` returned 200.
Its first candidate was the correct real missions file:

```json
{"root":"/home/ubuntu/Coding/missions","path":"missions/fleet-refit/artifacts/conductor/files-resolution-design.md","tier":"suffix","score":659}
```

The same response reported configured and healthy agent roots as `complete`
and the real `/home/ubuntu` root as `degraded` with a 3842-byte permission
diagnostic. Candidates from both the complete missions root and the degraded
home root remained present and ranked; one sick root did not mute healthy
answers.

`GET /api/files` read the real 10,389-byte ratified design without truncation.
A real 314,049-byte Zobrist JSON file returned exactly 262,144 content bytes
with `truncated:true`. `GET /api/files/tree` returned the name-sorted live
mission artifacts directory.

Refusal probes returned the pinned shapes:

- symlink `/tmp/herder-files-api-live-fixture/escape-hosts -> /etc/hosts`: 409
  with both requested and resolved paths in `detail`
- `.git/config`: 409 `refused by substrate`
- an absolute directory not in the current root universe: 404 `unknown root`

Live context enrichment was present in both response families:

```json
{"name":"impl-mevi","cwd":"/mnt/bench-nvme/herdr-worktrees/ai-config/files-api","git":{"branch":"files-api","remote_url":"https://github.com/yaminfinex/ai-config","worktree_of":"/home/ubuntu/Coding/ai-config"}}
```

The matching `GET /api/fleet` workspace (`w4W`) carried the same `cwd` and
`git` object. No `tools/herder/web/` source or committed artifact changed.
