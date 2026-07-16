package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"mish/internal/missionfs"
	"mish/internal/resolve"

	"github.com/spf13/cobra"
)

var askSubcommands = map[string]bool{
	"create": true, "view": true, "list": true, "reply": true, "settle": true,
	"close": true, "withdraw-with-citation": true, "link": true, "widen-membership": true,
}

type asksArgs struct {
	mission, operation, id, input string
	text                          bool
}

type asksOutput struct {
	OK         bool                 `json:"ok"`
	Verb       string               `json:"verb"`
	Slug       string               `json:"slug"`
	MissionDir string               `json:"mission_dir"`
	Entity     *missionfs.AskEntity `json:"entity,omitempty"`
	Warnings   []string             `json:"warnings,omitempty"`
}

type asksListOutput struct {
	OK         bool                  `json:"ok"`
	Verb       string                `json:"verb"`
	Slug       string                `json:"slug"`
	MissionDir string                `json:"mission_dir"`
	Counts     map[string]int        `json:"counts"`
	Entities   []missionfs.AskEntity `json:"entities"`
	Warnings   []string              `json:"warnings,omitempty"`
}

type createAskInput struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Actor       string `json:"actor"`
	AddressedTo string `json:"addressed_to"`
	Expects     string `json:"expects"`
	Blocking    *struct {
		Fact string `json:"fact"`
	} `json:"blocking"`
	Anchor  missionfs.TypedRef   `json:"anchor"`
	Links   []missionfs.TypedRef `json:"links"`
	Framing missionfs.Framing    `json:"framing"`
	Ruling  *struct {
		Prose string `json:"prose"`
	} `json:"ruling"`
}

type commonMutation struct {
	Actor       string `json:"actor"`
	IfUpdatedAt string `json:"if_updated_at"`
}
type replyInput struct {
	commonMutation
	Prose string `json:"prose"`
}
type settleInput struct {
	commonMutation
	Choice string `json:"choice"`
	Prose  string `json:"prose"`
}
type closeInput struct {
	commonMutation
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}
type withdrawInput struct {
	commonMutation
	Outcome  string             `json:"outcome"`
	Reason   string             `json:"reason"`
	Citation missionfs.TypedRef `json:"citation"`
}
type linkInput struct {
	commonMutation
	Link      missionfs.TypedRef `json:"link"`
	SetAnchor bool               `json:"set_anchor"`
}
type widenInput struct {
	commonMutation
	Member string `json:"member"`
}
type listInput struct {
	Kind    string              `json:"kind"`
	State   string              `json:"state"`
	Expects []string            `json:"expects"`
	Anchor  *missionfs.TypedRef `json:"anchor"`
	Member  string              `json:"member"`
}

func newAsksCommand(d deps) *cobra.Command {
	cmd := &cobra.Command{Use: "asks [--mission <slug>] <subcommand>", Short: "Manage mission asks and rulings", SilenceUsage: true, DisableFlagParsing: true, RunE: func(cmd *cobra.Command, args []string) error {
		if slicesContains(args, "--help") || slicesContains(args, "-h") || len(args) == 0 {
			return cmd.Help()
		}
		parsed, err := parseAsksArgs(args)
		if err != nil {
			return err
		}
		return withRefusalText(runAsks(cmd, d, parsed), parsed.text)
	}}
	attachHelp(cmd, asksHelpText)
	return cmd
}

func parseAsksArgs(args []string) (asksArgs, error) {
	var a asksArgs
	before := true
	for i := 0; i < len(args); i++ {
		x := args[i]
		if before {
			if x == "--mission" {
				if i+1 >= len(args) {
					return a, usageError{fmt.Errorf("mish asks: --mission requires a value")}
				}
				i++
				a.mission = args[i]
				continue
			}
			if strings.HasPrefix(x, "--mission=") {
				a.mission = strings.TrimPrefix(x, "--mission=")
				continue
			}
			if strings.HasPrefix(x, "-") {
				return a, usageError{fmt.Errorf("mish asks: unknown flag %s", x)}
			}
			a.operation = x
			before = false
			continue
		}
		switch {
		case x == "--input":
			if i+1 >= len(args) {
				return a, usageError{fmt.Errorf("mish asks %s: --input requires a value", a.operation)}
			}
			i++
			a.input = args[i]
		case strings.HasPrefix(x, "--input="):
			a.input = strings.TrimPrefix(x, "--input=")
		case x == "--text":
			a.text = true
		case strings.HasPrefix(x, "-"):
			return a, usageError{fmt.Errorf("mish asks %s: unknown flag %s", a.operation, x)}
		case a.id == "":
			a.id = x
		default:
			return a, usageError{fmt.Errorf("mish asks %s: unexpected argument %s", a.operation, x)}
		}
	}
	if !askSubcommands[a.operation] {
		return a, usageError{fmt.Errorf("mish asks: unknown subcommand %q — run 'mish asks --help' for usage", a.operation)}
	}
	needsID := a.operation != "create" && a.operation != "list"
	if needsID != (a.id != "") {
		if needsID {
			return a, usageError{fmt.Errorf("mish asks %s: expected one entity ID", a.operation)}
		}
		return a, usageError{fmt.Errorf("mish asks %s: unexpected entity ID", a.operation)}
	}
	if a.mission == "" { /* resolution may use cwd/marker */
	}
	return a, nil
}

func runAsks(cmd *cobra.Command, d deps, a asksArgs) error {
	result, err := resolveForAsks(d, a.mission, a.mission != "", a.operation)
	if err != nil {
		return err
	}
	manifest, _, err := missionfs.ReadManifest(result.MissionDir)
	if err != nil {
		return asksRefusal(a, result, "mission_unreadable", "could not read mission manifest", err.Error())
	}
	verb := "asks." + a.operation
	if a.operation == "view" {
		doc, err := missionfs.ReadAsk(result.MissionDir, a.id)
		if err != nil {
			return asksError(a, result, err)
		}
		warnings := doc.Warnings()
		if failure := missionfs.ValidateAskDocument(doc, manifest.Owner); failure != nil && failure.Kind != "unsupported_schema_version" {
			warnings = append(warnings, "invalid ask entity "+doc.Entity.ID+": "+failure.Message)
			sort.Strings(warnings)
		}
		return emitAsks(cmd, a, asksOutput{OK: true, Verb: verb, Slug: result.Slug, MissionDir: result.MissionDir, Entity: &doc.Entity, Warnings: warnings})
	}
	if a.operation == "list" {
		return runAsksList(cmd, d, a, result, manifest.Owner)
	}
	if a.input == "" {
		return usageError{fmt.Errorf("mish asks %s: --input is required", a.operation)}
	}
	input, err := readAsksInput(d, a.input)
	if err != nil {
		return asksRefusal(a, result, "invalid_input", "could not read one JSON input object", err.Error())
	}
	if a.operation == "create" {
		return runAsksCreate(cmd, d, a, result, manifest, input)
	}
	var common commonMutation
	if err := json.Unmarshal(input, &common); err != nil {
		return asksRefusal(a, result, "invalid_input", "input is not valid JSON", err.Error())
	}
	if common.Actor == "" {
		return asksRefusal(a, result, "missing_actor", "actor is required", "set actor to the durable mission participant performing the mutation")
	}
	now := d.clock()
	doc, err := missionfs.MutateAsk(result.MissionDir, a.id, common.IfUpdatedAt, now, func(doc *missionfs.AskDocument, stamp string) *missionfs.AskFailure {
		return applyAskMutation(a.operation, input, doc, manifest, stamp, func(now time.Time) (string, error) { return generateAskID(d, now) })
	})
	if err != nil {
		return asksError(a, result, err)
	}
	return emitAsks(cmd, a, asksOutput{OK: true, Verb: verb, Slug: result.Slug, MissionDir: result.MissionDir, Entity: &doc.Entity})
}

func resolveForAsks(d deps, mission string, set bool, operation string) (resolve.Result, error) {
	cwd, err := d.cwd()
	if err != nil {
		return resolve.Result{}, refusalError{verb: "asks." + operation, kind: "cwd_unavailable", message: "could not determine current directory", remedy: err.Error()}
	}
	r, err := resolve.Resolve(resolve.Options{MissionFlagSet: set, MissionFlag: mission, CWD: cwd, Env: func(k string) string {
		if k == "MISSIONS_REPO" {
			return d.missionsRepo
		}
		return d.env(k)
	}, FS: resolve.OSFS{}})
	if err == nil {
		return r, nil
	}
	var ref *resolve.Refusal
	if errors.As(err, &ref) {
		return r, refusalError{verb: "asks." + operation, kind: string(ref.Kind), message: ref.Reason, remedy: ref.Remedy, slug: ref.Slug, paths: ref.Paths}
	}
	return r, err
}

func readAsksInput(d deps, path string) ([]byte, error) {
	var r io.Reader = d.stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	dec := json.NewDecoder(r)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("input must contain exactly one JSON object")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	return raw, nil
}

func runAsksCreate(cmd *cobra.Command, d deps, a asksArgs, r resolve.Result, m missionfs.Manifest, raw []byte) error {
	var in createAskInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return asksRefusal(a, r, "invalid_input", "create input is invalid", err.Error())
	}
	if in.Actor == "" || in.AddressedTo == "" {
		return asksRefusal(a, r, "invalid_input", "actor and addressed_to are required", "provide both dyad participants")
	}
	if in.Kind == "" {
		in.Kind = "ask"
	}
	if in.ID == "" {
		id, err := generateAskID(d, d.clock())
		if err != nil {
			return asksRefusal(a, r, "id_generation_failed", "could not generate ask ID", err.Error())
		}
		in.ID = id
	}
	stamp := d.clock().UTC().Format(time.RFC3339Nano)
	if in.Framing.SubDecisions == nil {
		in.Framing.SubDecisions = []string{}
	}
	if in.Framing.Options == nil {
		in.Framing.Options = []missionfs.DecisionOption{}
	}
	members := []string{in.Actor}
	if in.AddressedTo != in.Actor {
		members = append(members, in.AddressedTo)
	}
	e := missionfs.AskEntity{Schema: missionfs.AskSchema, ID: in.ID, Kind: in.Kind, State: "open", Outcome: nil, Asker: in.Actor, AddressedTo: in.AddressedTo, CreatedAt: stamp, UpdatedAt: stamp, Expects: in.Expects, Anchor: in.Anchor, Links: nonNilRefs(in.Links), Members: members, Framing: in.Framing, Replies: []missionfs.Reply{}, RulingTrail: []missionfs.RulingEntry{}, Traces: []missionfs.Trace{}}
	if in.Blocking != nil {
		e.Blocking = &missionfs.Blocking{Fact: in.Blocking.Fact, Actor: in.Actor, At: stamp}
	}
	if in.Kind == "ruling" {
		if in.AddressedTo != m.Owner {
			return asksRefusal(a, r, "invalid_input", "direct ruling must be addressed to mission owner", "set addressed_to to mission.md owner")
		}
		if in.Ruling == nil || in.Ruling.Prose == "" {
			return asksRefusal(a, r, "invalid_input", "direct ruling requires ruling.prose", "provide the delegated ruling prose")
		}
		e.RulingTrail = append(e.RulingTrail, missionfs.RulingEntry{Prose: in.Ruling.Prose, Actor: in.Actor, At: stamp})
	}
	if failure := missionfs.ValidateAsk(e, m.Owner, m.Authority); failure != nil {
		return asksError(a, r, failure)
	}
	doc := &missionfs.AskDocument{Entity: e}
	if err := missionfs.WriteNewAsk(r.MissionDir, doc); err != nil {
		return asksError(a, r, err)
	}
	return emitAsks(cmd, a, asksOutput{OK: true, Verb: "asks.create", Slug: r.Slug, MissionDir: r.MissionDir, Entity: &doc.Entity})
}

func runAsksList(cmd *cobra.Command, d deps, a asksArgs, r resolve.Result, owner string) error {
	var filter listInput
	if a.input != "" {
		raw, err := readAsksInput(d, a.input)
		if err != nil {
			return asksRefusal(a, r, "invalid_input", "could not read list filters", err.Error())
		}
		if err := json.Unmarshal(raw, &filter); err != nil {
			return asksRefusal(a, r, "invalid_input", "list filters are invalid", err.Error())
		}
	}
	if filter.Kind != "" && !oneOfCLI(filter.Kind, "ask", "ruling") || filter.State != "" && !oneOfCLI(filter.State, "open", "closed") {
		return asksRefusal(a, r, "invalid_filter", "list filter vocabulary is invalid", "use documented kind and state values")
	}
	for _, expects := range filter.Expects {
		if !oneOfCLI(expects, "decide", "reply", "act", "read", "") {
			return asksRefusal(a, r, "invalid_filter", "list expects filter is invalid", "use documented expects values")
		}
	}
	if filter.Anchor != nil && !validCLIRef(*filter.Anchor, false) {
		return asksRefusal(a, r, "invalid_filter", "list anchor filter is invalid", "use an anchor-ladder typed reference")
	}
	scan := missionfs.ScanAsks(r.MissionDir, owner)
	entities := make([]missionfs.AskEntity, 0, len(scan.Entities))
	counts := map[string]int{"open": 0, "closed": 0}
	for _, e := range scan.Entities {
		if !matchesAsk(e, filter) {
			continue
		}
		entities = append(entities, e)
		if e.State == "open" || e.State == "closed" {
			counts[e.State]++
		}
	}
	if a.text {
		for _, e := range entities {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s %s\n", e.ID, e.Kind, e.State, e.Expects)
		}
		return nil
	}
	emitJSON(cmd.OutOrStdout(), asksListOutput{OK: true, Verb: "asks.list", Slug: r.Slug, MissionDir: r.MissionDir, Counts: counts, Entities: entities, Warnings: scan.Warnings})
	return nil
}

func matchesAsk(e missionfs.AskEntity, f listInput) bool {
	if f.Kind != "" && e.Kind != f.Kind || f.State != "" && e.State != f.State || f.Member != "" && !contains(e.Members, f.Member) {
		return false
	}
	if len(f.Expects) > 0 && !contains(f.Expects, e.Expects) {
		return false
	}
	if f.Anchor != nil && (e.Anchor != *f.Anchor) {
		return false
	}
	return true
}

func applyAskMutation(op string, raw []byte, doc *missionfs.AskDocument, m missionfs.Manifest, at string, idGen func(time.Time) (string, error)) *missionfs.AskFailure {
	fail := func(kind, msg, remedy string) *missionfs.AskFailure {
		return &missionfs.AskFailure{Kind: kind, Message: msg, Remedy: remedy}
	}
	closed := func() *missionfs.AskFailure {
		if doc.Entity.State != "open" {
			return fail("invalid_transition", "closed entity cannot be mutated by this operation", "read the entity and choose an operation valid for its current state")
		}
		return nil
	}
	if failure := missionfs.ValidateAsk(doc.Entity, m.Owner, m.Authority); failure != nil {
		return failure
	}
	switch op {
	case "reply":
		var in replyInput
		if json.Unmarshal(raw, &in) != nil || in.Prose == "" {
			return fail("invalid_input", "reply prose is required", "provide non-empty prose")
		}
		if !contains(doc.Entity.Members, in.Actor) {
			return fail("actor_not_member", "only a current member may reply", "ask the owner to widen membership")
		}
		id, err := idGen(parseStamp(at))
		if err != nil {
			return fail("id_generation_failed", "could not generate reply ID", err.Error())
		}
		missionfs.AppendReply(doc, missionfs.Reply{ID: strings.Replace(id, "ask-", "reply-", 1), Actor: in.Actor, At: at, Prose: in.Prose})
	case "settle":
		if x := closed(); x != nil {
			return x
		}
		var in settleInput
		if json.Unmarshal(raw, &in) != nil || in.Prose == "" {
			return fail("invalid_input", "settlement prose is required", "provide the ruling or acknowledgment prose")
		}
		if in.Actor != doc.Entity.AddressedTo {
			return fail("actor_not_addressee", "only the addressee may settle", "ask the addressed participant to settle")
		}
		if in.Choice != "" && !optionExists(doc.Entity.Framing.Options, in.Choice) {
			return fail("invalid_choice", "choice is not one of the presented options", "choose a presented option ID")
		}
		missionfs.AppendSettlementRuling(doc, missionfs.RulingEntry{Question: doc.Entity.Framing.Question, OptionsAsPresented: doc.Entity.Framing.Options, Choice: in.Choice, Prose: in.Prose, Actor: in.Actor, At: at})
		out := "settled"
		doc.Entity.Kind = "ruling"
		doc.Entity.State = "closed"
		doc.Entity.Outcome = &out
		missionfs.SetRaw(doc, "kind", "ruling")
		missionfs.SetRaw(doc, "state", "closed")
		missionfs.SetRaw(doc, "outcome", out)
	case "close":
		if x := closed(); x != nil {
			return x
		}
		var in closeInput
		if json.Unmarshal(raw, &in) != nil || !oneOfCLI(in.Outcome, "no-action", "superseded") || in.Reason == "" {
			return fail("invalid_input", "close requires outcome no-action or superseded and a reason", "provide the non-settlement close fields")
		}
		allowed := in.Actor == doc.Entity.AddressedTo || in.Actor == m.Owner
		if in.Actor == m.Authority && in.Actor != m.Owner && oneOfCLI(doc.Entity.Expects, "decide", "reply") {
			allowed = false
		}
		if in.Actor == m.Authority && oneOfCLI(doc.Entity.Expects, "", "act", "read") {
			allowed = true
		}
		if !allowed {
			return fail("actor_not_authorized", "actor may not close this entity", "use the addressee or owner; decide/reply authority withdrawal requires a citation")
		}
		doc.Entity.State = "closed"
		doc.Entity.Outcome = &in.Outcome
		missionfs.SetRaw(doc, "state", "closed")
		missionfs.SetRaw(doc, "outcome", in.Outcome)
		missionfs.AppendTrace(doc, missionfs.Trace{Action: "close", Actor: in.Actor, At: at, Outcome: in.Outcome, Reason: in.Reason})
	case "withdraw-with-citation":
		if x := closed(); x != nil {
			return x
		}
		var in withdrawInput
		if json.Unmarshal(raw, &in) != nil || !oneOfCLI(in.Outcome, "no-action", "superseded") || in.Reason == "" || in.Citation.Type == "" || in.Citation.Ref == "" {
			return fail("invalid_input", "withdrawal requires outcome, reason, and typed citation", "provide a cited no-action or superseded withdrawal")
		}
		if !validCLIRef(in.Citation, true) {
			return fail("invalid_citation", "withdrawal citation type is invalid", "use an allowed typed reference")
		}
		if doc.Entity.Kind != "ask" {
			return fail("invalid_transition", "only an open ask may be withdrawn", "use an operation valid for a ruling")
		}
		if in.Actor != m.Authority {
			return fail("actor_not_authority", "only mission authority may withdraw", "use the authority recorded in mission.md")
		}
		for _, reply := range doc.Entity.Replies {
			if reply.Actor == doc.Entity.AddressedTo {
				return fail("answered_ask", "an ask with an addressee reply cannot be withdrawn", "settle or close it with the addressee")
			}
		}
		doc.Entity.State = "closed"
		doc.Entity.Outcome = &in.Outcome
		missionfs.SetRaw(doc, "state", "closed")
		missionfs.SetRaw(doc, "outcome", in.Outcome)
		missionfs.AppendTrace(doc, missionfs.Trace{Action: "withdraw", Actor: in.Actor, At: at, Outcome: in.Outcome, Reason: in.Reason, Citation: &in.Citation})
	case "link":
		var in linkInput
		if json.Unmarshal(raw, &in) != nil || in.Link.Type == "" || in.Link.Ref == "" {
			return fail("invalid_input", "typed link is required", "provide link.type and link.ref")
		}
		if !contains(doc.Entity.Members, in.Actor) && in.Actor != m.Authority {
			return fail("actor_not_authorized", "only a member or mission authority may link", "use an authorized actor")
		}
		if !validCLIRef(in.Link, true) {
			return fail("invalid_link", "link type is invalid", "use task, phase, milestone, artifact, thread, mission, or entity")
		}
		if !refContains(doc.Entity.Links, in.Link) {
			missionfs.AppendLink(doc, in.Link)
		}
		if in.SetAnchor {
			if !validCLIRef(in.Link, false) {
				return fail("invalid_anchor", "entity links cannot become anchors", "use an anchor-ladder link")
			}
			doc.Entity.Anchor = in.Link
			missionfs.SetAnchor(doc, in.Link)
		}
	case "widen-membership":
		if x := closed(); x != nil {
			return x
		}
		var in widenInput
		if json.Unmarshal(raw, &in) != nil || in.Member == "" {
			return fail("invalid_input", "member is required", "provide the durable participant label")
		}
		if in.Actor != m.Owner {
			return fail("actor_not_owner", "only mission owner may widen membership", "use the owner recorded in mission.md")
		}
		if !contains(doc.Entity.Members, in.Member) {
			missionfs.AppendMember(doc, in.Member)
			missionfs.AppendTrace(doc, missionfs.Trace{Action: "widen-membership", Actor: in.Actor, At: at, Member: in.Member})
		}
	}
	return nil
}

func asksError(a asksArgs, r resolve.Result, err error) error {
	var f *missionfs.AskFailure
	if errors.As(err, &f) {
		return asksRefusal(a, r, f.Kind, f.Message, f.Remedy)
	}
	return asksRefusal(a, r, "asks_io_failed", "could not access asks storage", err.Error())
}
func asksRefusal(a asksArgs, r resolve.Result, kind, msg, remedy string) error {
	return refusalError{verb: "asks." + a.operation, kind: kind, message: msg, remedy: remedy, slug: r.Slug, text: a.text}
}
func emitAsks(cmd *cobra.Command, a asksArgs, out asksOutput) error {
	if a.text {
		if out.Entity != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s %s\n", out.Entity.ID, out.Entity.Kind, out.Entity.State, out.Entity.Expects)
		}
		return nil
	}
	emitJSON(cmd.OutOrStdout(), out)
	return nil
}
func nonNilRefs(v []missionfs.TypedRef) []missionfs.TypedRef {
	if v == nil {
		return []missionfs.TypedRef{}
	}
	return v
}
func contains(v []string, x string) bool {
	for _, s := range v {
		if s == x {
			return true
		}
	}
	return false
}
func oneOfCLI(v string, x ...string) bool { return contains(x, v) }
func optionExists(v []missionfs.DecisionOption, id string) bool {
	for _, o := range v {
		if o.ID == id {
			return true
		}
	}
	return false
}
func refContains(v []missionfs.TypedRef, x missionfs.TypedRef) bool {
	for _, r := range v {
		if r == x {
			return true
		}
	}
	return false
}
func validCLIRef(r missionfs.TypedRef, entity bool) bool {
	return r.Ref != "" && (oneOfCLI(r.Type, "task", "phase", "milestone", "artifact", "thread", "mission") || (entity && r.Type == "entity" && missionfs.ValidAskID(r.Ref)))
}
func parseStamp(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }

func generateAskID(d deps, now time.Time) (string, error) {
	if d.askID != nil {
		return d.askID(now)
	}
	return missionfs.GenerateAskID(now)
}
