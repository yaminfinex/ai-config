package cli

import "fmt"

// renderHelp is the byte-exact layout of every `bottle <command> --help`: the
// one-line usage, the summary sentence, then the command's examples-and-pitfalls
// body. Goldens pin the result, so the format is deliberately stable.
func renderHelp(usage, summary, body string) string {
	return fmt.Sprintf("Usage: %s\n\n%s.\n\n%s", usage, summary, body)
}

// Per-command help bodies. Each is usage + 2–3 copy-pasteable examples + the
// pitfalls that actually bite — nothing else. They document the *landed*
// behavior of the command funcs, not aspirations.

const createHelp = `Examples:
  bottle create auth-expert                       # snapshot the current session
  bottle create auth-expert --at 12               # rewind: freeze through turn 12
  bottle create review --last --note "post-review baseline"

Pitfalls:
  - Self-bottle trims the in-flight turn. Bottling your own live session cuts at
    the last *completed* turn, so the running ` + "`bottle create`" + ` call (and any
    trailing unfinished turn) is dropped — an honest snapshot, by design.
  - Put <name> before --at N. A bare --at opens an interactive turn picker; a
    number right after it is read as the turn, so write
    ` + "`bottle create foo --at 3`" + `, not ` + "`bottle create --at 3 foo`" + `. The picker
    needs a TTY — pass --at N to rewind non-interactively.
  - --attach refuses sensitive-looking names (.env*, *secret*, *credential*,
    id_rsa*, *.pem) and files outside the cwd without --force; attachments enter
    the store's permanent git history.
`

const decantHelp = `Examples:
  bottle decant auth-expert                       # resume in this terminal
  bottle decant auth-expert@2 --pane right        # open in a herdr split
  bottle decant auth-expert --cwd ~/work/api --prompt "continue the migration"

Pitfalls:
  - Decant chdirs to the bottle's recorded cwd before resuming (Claude sessions
    are cwd-scoped). If that directory is gone, decant refuses up front — pass
    --cwd PATH to run elsewhere. Nothing is written before the cwd check passes.
  - Permissions are safe-by-default: the seeded agent still prompts. --yolo opts
    out (--dangerously-skip-permissions), matching interactive decant even though
    herder-spawn's own default differs.
  - --pane right|below needs a herdr environment (it shells out to herder-spawn).
`

const rebottleHelp = `Examples:
  bottle rebottle                                 # re-bottle the live session under its parent's name
  bottle rebottle auth-expert-v2                  # ... under a new name (new lineage, parent still recorded)
  bottle rebottle --session <id> --note "after fixing the deadlock"

Pitfalls:
  - rebottle only works on a *decanted* session — one this tool produced and
    recorded. A session that was never decanted has no parent to inherit; it is
    refused with a pointer to ` + "`bottle create`" + ` for a fresh root bottle.
  - Re-bottling the live session self-trims the in-flight turn, exactly like a
    self-bottle (` + "`bottle create`" + `).
  - There is no --at; rebottle freezes the whole decanted session (self-trim
    aside). Rewind at create time instead.
`

const listHelp = `Examples:
  bottle list

Pitfalls:
  - An empty store is not an error: it prints a one-line hint and exits 0.
  - Columns are NAME / LATEST / VERSIONS / AGE / NOTE; AGE is the latest
    version's age, humanized (5s, 3m, 2h, 4d).
`

const logHelp = `Examples:
  bottle log auth-expert

Pitfalls:
  - Output is newest-first. A rebottled version renders its provenance, e.g.
    ` + "`@2 ← decant of auth-expert@1 (session 9f2c…), 2026-06-10`" + `.
  - Bracketed tags ([compacted], [rewound-into-parent], …) come from the frozen
    transcript; a parent removed since is shown as (deleted).
`

const showHelp = `Examples:
  bottle show auth-expert                          # metadata + last 5 turns
  bottle show auth-expert@2 --turns 20

Pitfalls:
  - --turns N previews the last N turns of the frozen transcript (default 5);
    it does not change the bottle.
  - "cut at X of Y" reports where the snapshot was taken — Y is the source's
    total turns, X the turn it was frozen at.
`

const renameHelp = `Examples:
  bottle rename auth auth-expert

Pitfalls:
  - Renames every version of the name at once. It is a registry-only move —
    bottle directories do not move and lineage (parent links) survives.
  - Refused if the target name already exists.
`

const noteHelp = `Examples:
  bottle note auth-expert "trusted baseline — resume here"
  bottle note auth-expert@2 "pinned snapshot"

Pitfalls:
  - The note is the only sanctioned mutation of an otherwise-immutable bottle.
  - An unpinned name edits the *latest* version; pin with name@v to annotate an
    older one. Multi-word notes need not be quoted (trailing args are joined).
`

const pruneHelp = `Examples:
  bottle prune

Pitfalls:
  - Prune only drops decant-map entries whose seeded session files are gone
    (Claude garbage-collected them). Bottles themselves are never touched.
  - It reports the count removed; zero dead decants is a normal, successful run.
`

const rmHelp = `Examples:
  bottle rm old-expert@1                           # one version (interactive y/N)
  bottle rm old-expert --force                      # whole name, no prompt

Pitfalls:
  - rm always prints the git-history retention warning: the bottle leaves the
    registry and live store, but its transcript survives in ~/.bottles git
    history until you rewrite it (e.g. git -C ~/.bottles with git filter-repo).
  - Without --force, a non-interactive stdin is refused — agents and scripts
    must pass --force. A bare name removes every version; name@v removes one.
`

const artifactsHelp = `Examples:
  bottle artifacts auth-expert                      # list attached files
  bottle artifacts auth-expert --extract ./out      # extract into ./out

Pitfalls:
  - Extraction never overwrites: if any target path already exists it refuses up
    front, names the collision, and writes nothing.
  - With --extract and no DIR, files land under ./bottle-artifacts/<name>@<v>/.
`

const syncHelp = `Examples:
  bottle sync --remote git@github.com:me/bottles.git   # first run: configure origin, then sync
  bottle sync                                          # thereafter: fetch, merge, push

Pitfalls:
  - The remote must be PRIVATE. Bottles carry full transcripts — keys, file
    contents, anything said in session — and sync pushes every one of them.
  - Name collisions auto-rename the newer bottle: when both machines created the
    same name, the older ` + "`created`" + ` keeps it and the newer bottle (with its
    descendant versions) moves to the first free suffix (auth-expert-2). Each
    move prints as ` + "`renamed: old → new (name collision)`" + `.
  - ` + "`bottle rm`" + ` does not expunge git history: a removed bottle's transcript
    survives in the remote's history until that history is rewritten and
    force-pushed.
`
