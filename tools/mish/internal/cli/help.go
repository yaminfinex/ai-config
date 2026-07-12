package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type verbHelpRow struct {
	name    string
	summary string
}

var rootHelpVerbs = []verbHelpRow{
	{name: "new", summary: "scaffold a mission and stamp authority/owner"},
	{name: "backlog", summary: "run allowlisted Backlog.md commands in one mission"},
	{name: "status", summary: "report mission health and overview read-only"},
}

const rootHelpText = `Usage: mish <verb> [args]

mish manages missions: durable work homes under $MISSIONS_REPO/missions/<slug>/.
A mission is one directory containing mission.md, a pinned Backlog.md board, and artifacts/.
The slug is the directory name and must match mission.md frontmatter. A .mission marker points
a project worktree at a mission; markers never nest.

Concepts:
  authority  label with write authority for mission state and board etiquette
  owner      human owner stamped by --owner, SESSION_OWNER, then OS user
  custody    git commit prose that records adoption, harvest, rename, closeout, and deletion
  marker     .mission file containing a slug, deleted when the worktree no longer participates

Verbs:
  new       scaffold a mission and stamp authority/owner
  backlog   run allowlisted Backlog.md commands in one mission
  status    report mission health and overview read-only

Git rhythm: pull before creating missions, task creation, board restructuring, or manifest
edits; commit early at the mission-subtree grain; push when a unit lands. mish never writes git.

Custody commits use: mission(<slug>): <verb> <summary>
Open custody verbs include new, adopt, harvest, delete, rename, and close. Optional trailers:
Mission-Source, Mission-Dest, Mission-Agent.

Run 'mish <verb> --help' for the working doctrine on each verb.
`

const newHelpText = `Usage: mish new <slug> [--title T] [--authority A] [--owner O] [--no-marker]

Use 'mish new -h' or 'mish new --help' for this help; 'mish help new' is also available.

Scaffold missions/<slug>/ under $MISSIONS_REPO with mission.md, a pinned Backlog.md board,
and an empty artifacts/ directory. The slug must be lowercase letters, digits, and single
hyphens; it becomes both the directory name and mission: frontmatter.

Authority and owner:
  authority  --authority, else the invoking OS user
  owner      --owner, else SESSION_OWNER, else the invoking OS user

On success, mish prints both stamped values and their source so a wrong stamp is visible at
birth. Correcting one later is an ordinary manifest edit owned by the authority.

Markers:
  new writes .mission in the invoking cwd unless --no-marker is set or cwd is inside the
  missions repo. Existing same-slug markers are no-ops. Any marker on the cwd-to-root chain
  that names a different slug refuses; remove it or pass --no-marker. Markers never nest.

Git and custody:
  new performs no git operations. The scaffold should be committed by the caller with:
  mission(<slug>): new <summary>
  Use Mission-Agent when the acting agent label matters.

Closeout, rename, and marker hygiene:
  Closing later means final board states, a harvest pass, Closeout prose in mission.md,
  status: closed, custody-rhythm review, and a close custody commit. Renames are authority
  acts outside the CLI: choose a new slug (§4.3 rules) whose directory does not exist, git mv
  the directory, edit mission: and backlog/config.yml project_name, fix stale markers, then
  make one rename custody commit.
`

const backlogHelpText = `Usage: mish backlog [--mission <slug>] <subcommand> [args...]

Resolve one mission, verify backlog/config.yml exists, check the first forwarded subcommand
against this closed allowlist, then exec Backlog.md with cwd pinned to the mission directory.
Arguments, stdin/stdout/stderr, and exit code pass through verbatim after the guard.

Allowed subcommands:
  task        Task CRUD, notes, status, and references (--ref)
  tasks       Alias for task
  draft       Draft task workflow
  board       Kanban render
  search      Read-only index search
  overview    Read-only project stats
  sequence    Read-only dependency sequences
  doc         Board-internal docs inside the mission dir
  decision    Board-internal decisions inside the mission dir
  milestone   Board-internal grouping
  milestones  Alias for milestone
  cleanup     Ages Done tasks into completed; authority etiquette applies

Excluded rationale:
  init          re-initializing inside a mission damages the scaffold
  config        pinned keys are invariant; deliberate tuning is an authority file edit
  agents        writes instruction files at the board root and litters the shared repo
  browser       settings endpoint rewrites config.yml pins; dropped until read-only exists
  completion    no mission use case yet
  instructions  no mission use case yet
  mcp           no mission use case yet

Help matches the surface: bare 'mish backlog', 'mish backlog help', and leading -h/--help
print this wrapper help. Per-subcommand help passes through, for example:
  mish backlog task --help

References:
  Cross-references live in Backlog.md's native references list via --ref at task create/edit.
  Suggested opaque shapes are <repo>@<sha>, branch:<repo>#<name>, pr:<repo>#<n>,
  session:<label>, and agent:<label>. mish validates none of them.

Replace edge:
  On Backlog.md 1.47.x, 'task edit --ref' replaces the whole references list rather than
  appending. Read the current references first and re-set the full intended set.

Git rhythm and custody:
  Pull before board restructuring or task creation, commit early at mission-subtree grain,
  and push when a unit lands. External effects such as a PR merged or deploy shipped belong
  in task notes plus references. mish never writes git.
`

const statusHelpText = `Usage: mish status [--mission <slug> | --all]

Read mission state without mutating files. With a resolved context, status prints one mission:
manifest status/authority/owner/created, task counts in the board's configured status order,
artifact count/newest file, and warnings. With --all, or from inside $MISSIONS_REPO with no
specific context, it scans every mission directory including closed missions.

Warnings mean:
  pinned board key drifted from the mission invariants
  mission: frontmatter does not match the directory slug
  mission.md has unknown keys or status is not active/closed
  duplicate task IDs appear on the board
  board or artifacts/ is missing
  mission subtree has uncommitted or unpushed git changes

Git staleness:
  The git signal is read-only and silently skipped when the missions repo is not git or has
  no upstream. It is a prompt to pull/push, not a source of truth for ownership.

Recency:
  UPDATED and artifact age use filesystem mtimes, never git history. They are node-local:
  clone and pull re-stamp mtimes, so status reports when changes reached this clone, not when
  the work happened.

Closeout checklist:
  Before setting status: closed, the authority confirms every task is Done or explicitly
  abandoned, harvests durable outputs, writes Closeout prose in mission.md, reviews
  'git log -- missions/<slug>/' for custody coverage, then commits:
  mission(<slug>): close <summary>

Rename and marker hygiene:
  Rename outside the CLI by choosing a new slug (§4.3 rules) whose directory does not exist,
  then git mv, mission: and project_name edits, marker fixes, and one rename custody commit.
  Keep one .mission marker per directory chain; delete stale markers when the worktree no
  longer participates.
`

func rootHelp() string {
	return rootHelpText
}

func attachHelp(cmd *cobra.Command, text string) {
	cmd.Long = strings.TrimSuffix(text, "\n")
	cmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		_, _ = io.WriteString(cmd.OutOrStdout(), text)
	})
}

func rootHelpVerbNames() []string {
	names := make([]string, 0, len(rootHelpVerbs))
	for _, row := range rootHelpVerbs {
		names = append(names, row.name)
	}
	sort.Strings(names)
	return names
}

func backlogHelp() string {
	return backlogHelpText
}

func validateBacklogHelpAllowlist() error {
	help := backlogHelp()
	for _, entry := range backlogAllowlist {
		if !strings.Contains(help, fmt.Sprintf("\n  %-11s", entry.name)) && !strings.Contains(help, "\n  "+entry.name+" ") {
			return fmt.Errorf("backlog help missing allowed subcommand %q", entry.name)
		}
	}
	return nil
}
