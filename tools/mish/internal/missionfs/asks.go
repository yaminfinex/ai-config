package missionfs

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	AsksConfigSchema = "mish.asks/v1"
	AskSchema        = "mish.ask/v1"
)

var askIDPattern = regexp.MustCompile(`^ask-[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var replyIDPattern = regexp.MustCompile(`^reply-[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type TypedRef struct {
	Type string `json:"type" yaml:"type"`
	Ref  string `json:"ref" yaml:"ref"`
}

type Blocking struct {
	Fact  string `json:"fact" yaml:"fact"`
	Actor string `json:"actor" yaml:"actor"`
	At    string `json:"at" yaml:"at"`
}

type DecisionOption struct {
	ID          string `json:"id" yaml:"id"`
	Label       string `json:"label" yaml:"label"`
	Cost        string `json:"cost" yaml:"cost"`
	Risk        string `json:"risk" yaml:"risk"`
	BlastRadius string `json:"blast_radius" yaml:"blast_radius"`
}

type Recommendation struct {
	Choice string `json:"choice" yaml:"choice"`
	Reason string `json:"reason" yaml:"reason"`
}

type Framing struct {
	Context        string           `json:"context" yaml:"context"`
	Question       string           `json:"question" yaml:"question"`
	SubDecisions   []string         `json:"sub_decisions" yaml:"sub_decisions"`
	Options        []DecisionOption `json:"options" yaml:"options"`
	Recommendation *Recommendation  `json:"recommendation" yaml:"recommendation"`
	DoNothing      string           `json:"do_nothing" yaml:"do_nothing"`
}

type Reply struct {
	ID    string `json:"id" yaml:"id"`
	Actor string `json:"actor" yaml:"actor"`
	At    string `json:"at" yaml:"at"`
	Prose string `json:"prose" yaml:"prose"`
}

type RulingEntry struct {
	Question           string           `json:"question,omitempty" yaml:"question,omitempty"`
	OptionsAsPresented []DecisionOption `json:"options_as_presented,omitempty" yaml:"options_as_presented,omitempty"`
	Choice             string           `json:"choice,omitempty" yaml:"choice,omitempty"`
	Prose              string           `json:"prose" yaml:"prose"`
	Actor              string           `json:"actor" yaml:"actor"`
	At                 string           `json:"at" yaml:"at"`
}

type Trace struct {
	Action   string    `json:"action" yaml:"action"`
	Actor    string    `json:"actor" yaml:"actor"`
	At       string    `json:"at" yaml:"at"`
	Outcome  string    `json:"outcome,omitempty" yaml:"outcome,omitempty"`
	Reason   string    `json:"reason,omitempty" yaml:"reason,omitempty"`
	Citation *TypedRef `json:"citation,omitempty" yaml:"citation,omitempty"`
	Member   string    `json:"member,omitempty" yaml:"member,omitempty"`
}

type AskEntity struct {
	Schema      string        `json:"schema" yaml:"schema"`
	ID          string        `json:"id" yaml:"id"`
	Kind        string        `json:"kind" yaml:"kind"`
	State       string        `json:"state" yaml:"state"`
	Outcome     *string       `json:"outcome" yaml:"outcome"`
	Asker       string        `json:"asker" yaml:"asker"`
	AddressedTo string        `json:"addressed_to" yaml:"addressed_to"`
	CreatedAt   string        `json:"created_at" yaml:"created_at"`
	UpdatedAt   string        `json:"updated_at" yaml:"updated_at"`
	Expects     string        `json:"expects" yaml:"expects"`
	Blocking    *Blocking     `json:"blocking,omitempty" yaml:"blocking,omitempty"`
	Anchor      TypedRef      `json:"anchor" yaml:"anchor"`
	Links       []TypedRef    `json:"links" yaml:"links"`
	Members     []string      `json:"members" yaml:"members"`
	Framing     Framing       `json:"framing" yaml:"framing"`
	Replies     []Reply       `json:"replies" yaml:"replies"`
	RulingTrail []RulingEntry `json:"ruling_trail" yaml:"ruling_trail"`
	Traces      []Trace       `json:"traces" yaml:"traces"`
}

type AskDocument struct {
	Entity   AskEntity
	Body     []byte
	raw      map[string]any
	path     string
	warnings []string
}

func (d *AskDocument) Warnings() []string { return append([]string(nil), d.warnings...) }

type AskStateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type AsksScan struct {
	Available bool
	Counts    []AskStateCount
	Entities  []AskEntity
	Warnings  []string
}

type AskFailure struct {
	Kind, Message, Remedy string
}

func (e *AskFailure) Error() string { return e.Message }

func WriteAsksScaffold(missionDir string) error {
	dir := filepath.Join(missionDir, "asks")
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "config.yml"), []byte("schema: mish.asks/v1\nstates: [open, closed]\noutcomes: [settled, no-action, superseded]\n"))
}

func GenerateAskID(now time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(now.UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = 0x70 | (b[6] & 0x0f)
	b[8] = 0x80 | (b[8] & 0x3f)
	return fmt.Sprintf("ask-%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func ValidAskID(id string) bool { return askIDPattern.MatchString(id) }

func ScanAsks(missionDir string, owners ...string) AsksScan {
	dir := filepath.Join(missionDir, "asks")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return AsksScan{Counts: []AskStateCount{}, Entities: []AskEntity{}, Warnings: []string{}}
	}
	scan := AsksScan{Available: true, Counts: []AskStateCount{{State: "open"}, {State: "closed"}}, Entities: []AskEntity{}, Warnings: []string{}}
	if err := validateAsksConfig(dir); err != nil {
		scan.Warnings = append(scan.Warnings, err.Error())
	}
	entries, err := os.ReadDir(filepath.Join(dir, "entities"))
	if err != nil {
		if os.IsNotExist(err) {
			return scan
		}
		scan.Warnings = append(scan.Warnings, "asks entities unreadable: asks/entities")
		sort.Strings(scan.Warnings)
		return scan
	}
	seenIDs := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, "entities", entry.Name())
		doc, err := readAskPath(path)
		if err != nil {
			scan.Warnings = append(scan.Warnings, fmt.Sprintf("malformed ask entity: asks/entities/%s", entry.Name()))
			continue
		}
		if strings.TrimSuffix(entry.Name(), ".md") != doc.Entity.ID {
			scan.Warnings = append(scan.Warnings, fmt.Sprintf("ask filename/id mismatch: asks/entities/%s", entry.Name()))
		}
		if prior, exists := seenIDs[doc.Entity.ID]; exists {
			scan.Warnings = append(scan.Warnings, fmt.Sprintf("duplicate ask ID %s: asks/entities/%s and asks/entities/%s", doc.Entity.ID, prior, entry.Name()))
		} else {
			seenIDs[doc.Entity.ID] = entry.Name()
		}
		if doc.Entity.Schema == AskSchema {
			owner := ""
			if len(owners) > 0 {
				owner = owners[0]
			}
			if err := ValidateAskDocument(doc, owner); err != nil {
				scan.Warnings = append(scan.Warnings, fmt.Sprintf("invalid ask entity %s: %s", doc.Entity.ID, err.Message))
			}
		}
		scan.Entities = append(scan.Entities, doc.Entity)
		scan.Warnings = append(scan.Warnings, doc.warnings...)
	}
	sort.Slice(scan.Entities, func(i, j int) bool {
		if scan.Entities[i].CreatedAt == scan.Entities[j].CreatedAt {
			return scan.Entities[i].ID < scan.Entities[j].ID
		}
		a, aerr := time.Parse(time.RFC3339Nano, scan.Entities[i].CreatedAt)
		b, berr := time.Parse(time.RFC3339Nano, scan.Entities[j].CreatedAt)
		if aerr == nil && berr == nil {
			if a.Equal(b) {
				return scan.Entities[i].ID < scan.Entities[j].ID
			}
			return a.Before(b)
		}
		return scan.Entities[i].CreatedAt < scan.Entities[j].CreatedAt
	})
	for _, e := range scan.Entities {
		if e.State == "open" {
			scan.Counts[0].Count++
		} else if e.State == "closed" {
			scan.Counts[1].Count++
		}
	}
	sort.Strings(scan.Warnings)
	return scan
}

func validateAsksConfig(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		return fmt.Errorf("asks config unreadable: asks/config.yml")
	}
	var cfg struct {
		Schema   string   `yaml:"schema"`
		States   []string `yaml:"states"`
		Outcomes []string `yaml:"outcomes"`
	}
	if yaml.Unmarshal(data, &cfg) != nil || cfg.Schema != AsksConfigSchema || strings.Join(cfg.States, ",") != "open,closed" || strings.Join(cfg.Outcomes, ",") != "settled,no-action,superseded" {
		return fmt.Errorf("malformed asks config: asks/config.yml")
	}
	return nil
}

func ReadAsk(missionDir, id string) (*AskDocument, error) {
	if !ValidAskID(id) {
		return nil, &AskFailure{Kind: "invalid_id", Message: "ask ID is invalid", Remedy: "use an ask- UUIDv7 ID"}
	}
	doc, err := readAskPath(filepath.Join(missionDir, "asks", "entities", id+".md"))
	if os.IsNotExist(err) {
		return nil, &AskFailure{Kind: "entity_not_found", Message: fmt.Sprintf("ask entity %s was not found", id), Remedy: "list asks and choose an existing ID"}
	}
	if err == nil && doc.Entity.ID != id {
		return nil, &AskFailure{Kind: "filename_id_mismatch", Message: "ask filename and entity ID do not match", Remedy: "repair the entity filename or id before using it"}
	}
	return doc, err
}

func readAskPath(path string) (*AskDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	front, body, err := splitAskFrontmatter(data)
	if err != nil {
		return nil, err
	}
	raw := map[string]any{}
	if err := yaml.Unmarshal(front, &raw); err != nil {
		return nil, err
	}
	var entity AskEntity
	if err := yaml.Unmarshal(front, &entity); err != nil {
		return nil, err
	}
	doc := &AskDocument{Entity: entity, Body: body, raw: raw, path: path}
	if entity.Schema != AskSchema {
		doc.warnings = append(doc.warnings, "unsupported_schema_version: "+entity.ID)
	}
	return doc, nil
}

func splitAskFrontmatter(data []byte) ([]byte, []byte, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return nil, nil, errors.New("missing YAML frontmatter")
	}
	i := strings.Index(s[4:], "\n---")
	if i < 0 {
		return nil, nil, errors.New("unterminated YAML frontmatter")
	}
	i += 4
	end := i + 4
	if end < len(s) && s[end] == '\n' {
		end++
	}
	return []byte(s[4 : i+1]), []byte(s[end:]), nil
}

func ValidateAsk(e AskEntity, owner, authority string) *AskFailure {
	invalid := func(msg string) *AskFailure {
		return &AskFailure{Kind: "invalid_entity", Message: msg, Remedy: "repair the entity to match mish.ask/v1"}
	}
	if e.Schema != AskSchema {
		return &AskFailure{Kind: "unsupported_schema_version", Message: "ask schema is not writable", Remedy: "migrate the entity to mish.ask/v1 before mutating it"}
	}
	if !ValidAskID(e.ID) {
		return invalid("id must be an ask- UUIDv7")
	}
	if e.Kind != "ask" && e.Kind != "ruling" {
		return invalid("kind must be ask or ruling")
	}
	if e.State != "open" && e.State != "closed" {
		return invalid("state must be open or closed")
	}
	if e.State == "open" && e.Outcome != nil && *e.Outcome != "" {
		return invalid("open entity cannot have an outcome")
	}
	if e.State == "closed" && (e.Outcome == nil || !oneOf(*e.Outcome, "settled", "no-action", "superseded")) {
		return invalid("closed entity requires a valid outcome")
	}
	if e.Links == nil || e.Replies == nil || e.RulingTrail == nil || e.Traces == nil || e.Framing.SubDecisions == nil || e.Framing.Options == nil {
		return invalid("required collection fields must be arrays, not null")
	}
	if e.Asker == "" || e.AddressedTo == "" || e.CreatedAt == "" || e.UpdatedAt == "" {
		return invalid("required identity and timestamp fields must be non-empty")
	}
	if _, err := time.Parse(time.RFC3339Nano, e.CreatedAt); err != nil {
		return invalid("created_at must be RFC3339")
	}
	if _, err := time.Parse(time.RFC3339Nano, e.UpdatedAt); err != nil {
		return invalid("updated_at must be RFC3339")
	}
	if !oneOf(e.Expects, "", "decide", "reply", "act", "read") {
		return invalid("expects must be decide, reply, act, read, or empty")
	}
	if owner != "" && (e.Asker == owner) == (e.AddressedTo == owner) {
		return invalid("exactly one of asker and addressed_to must equal mission owner")
	}
	if len(e.Members) < 2 || e.Members[0] != e.Asker || e.Members[1] != e.AddressedTo || hasDuplicates(e.Members) {
		return invalid("members must begin with asker and addressed_to without duplicates")
	}
	if e.Blocking != nil && (e.Blocking.Fact == "" || e.Blocking.Actor == "" || e.Blocking.At == "") {
		return invalid("blocking must contain fact, actor, and at")
	}
	if e.Blocking != nil {
		if _, err := time.Parse(time.RFC3339Nano, e.Blocking.At); err != nil {
			return invalid("blocking at must be RFC3339")
		}
	}
	if !validRef(e.Anchor, false) {
		return invalid("anchor type or ref is invalid")
	}
	for _, link := range e.Links {
		if !validRef(link, true) {
			return invalid("link type or ref is invalid")
		}
	}
	for _, reply := range e.Replies {
		if !replyIDPattern.MatchString(reply.ID) || reply.Actor == "" || reply.At == "" || reply.Prose == "" {
			return invalid("reply entries require id, actor, at, and prose")
		}
		if _, err := time.Parse(time.RFC3339Nano, reply.At); err != nil {
			return invalid("reply at must be RFC3339")
		}
		if !oneOf(reply.Actor, e.Members...) {
			return invalid("reply actor must be a member")
		}
	}
	for _, ruling := range e.RulingTrail {
		if ruling.Actor == "" || ruling.At == "" || ruling.Prose == "" {
			return invalid("ruling trail entries require actor, at, and prose")
		}
		if _, err := time.Parse(time.RFC3339Nano, ruling.At); err != nil {
			return invalid("ruling at must be RFC3339")
		}
	}
	closureTraces := 0
	for _, trace := range e.Traces {
		if trace.Action == "" || trace.Actor == "" || trace.At == "" {
			return invalid("trace entries require action, actor, and at")
		}
		if !oneOf(trace.Action, "close", "withdraw", "widen-membership") {
			return invalid("trace action is invalid")
		}
		if _, err := time.Parse(time.RFC3339Nano, trace.At); err != nil {
			return invalid("trace at must be RFC3339")
		}
		switch trace.Action {
		case "close":
			closureTraces++
			if !oneOf(trace.Outcome, "no-action", "superseded") || trace.Reason == "" {
				return invalid("close trace requires outcome and reason")
			}
		case "withdraw":
			closureTraces++
			if !oneOf(trace.Outcome, "no-action", "superseded") || trace.Reason == "" || trace.Citation == nil || !validRef(*trace.Citation, true) {
				return invalid("withdraw trace requires outcome, reason, and citation")
			}
		case "widen-membership":
			if trace.Member == "" {
				return invalid("widen-membership trace requires member")
			}
			if !oneOf(trace.Member, e.Members...) {
				return invalid("widen-membership trace member must be in members")
			}
		}
		if oneOf(trace.Action, "close", "withdraw") && (e.State != "closed" || e.Outcome == nil || trace.Outcome != *e.Outcome) {
			return invalid("closure trace must match entity state and outcome")
		}
	}
	if e.Expects == "decide" {
		f := e.Framing
		if f.Context == "" || f.Question == "" || f.SubDecisions == nil || len(f.Options) < 2 || len(f.Options) > 4 || f.Recommendation == nil || f.DoNothing == "" {
			return invalid("decide framing is incomplete")
		}
		ids := map[string]bool{}
		for _, o := range f.Options {
			if o.ID == "" || o.Label == "" || o.Cost == "" || o.Risk == "" || o.BlastRadius == "" || ids[o.ID] {
				return invalid("decision options are incomplete or duplicate")
			}
			ids[o.ID] = true
		}
		if !ids[f.Recommendation.Choice] || f.Recommendation.Reason == "" {
			return invalid("recommendation must choose an option and explain why")
		}
	}
	if e.Kind == "ruling" && len(e.RulingTrail) == 0 {
		return invalid("ruling requires a ruling trail")
	}
	if e.Outcome != nil && *e.Outcome == "settled" && (e.Kind != "ruling" || len(e.RulingTrail) == 0 || closureTraces != 0) {
		return invalid("settled entity requires ruling kind and trail")
	}
	if e.State == "closed" && e.Outcome != nil && oneOf(*e.Outcome, "no-action", "superseded") && closureTraces != 1 {
		return invalid("non-settled closed entity requires exactly one matching closure trace")
	}
	if e.State == "open" && closureTraces != 0 {
		return invalid("open entity cannot contain a closure trace")
	}
	return nil
}

func ValidateAskDocument(doc *AskDocument, owner string) *AskFailure {
	required := []string{"schema", "id", "kind", "state", "outcome", "asker", "addressed_to", "created_at", "updated_at", "expects", "anchor", "links", "members", "framing", "replies", "ruling_trail", "traces"}
	for _, key := range required {
		if _, ok := doc.raw[key]; !ok {
			return &AskFailure{Kind: "invalid_entity", Message: "missing required field " + key, Remedy: "repair the entity to match mish.ask/v1"}
		}
	}
	return ValidateAsk(doc.Entity, owner, "")
}

func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}
func hasDuplicates(v []string) bool {
	seen := map[string]bool{}
	for _, x := range v {
		if seen[x] {
			return true
		}
		seen[x] = true
	}
	return false
}
func validRef(r TypedRef, entity bool) bool {
	if r.Ref == "" {
		return false
	}
	if oneOf(r.Type, "task", "phase", "milestone", "artifact", "thread", "mission") {
		return true
	}
	return entity && r.Type == "entity" && ValidAskID(r.Ref)
}

func WithAsksLock(missionDir string, fn func() error) error {
	dir := filepath.Join(missionDir, "asks")
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
		return err
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func WriteNewAsk(missionDir string, doc *AskDocument) error {
	return WithAsksLock(missionDir, func() error {
		if err := ensureAsksConfig(missionDir); err != nil {
			return err
		}
		path := filepath.Join(missionDir, "asks", "entities", doc.Entity.ID+".md")
		if existing, err := readAskPath(path); err == nil {
			if normalizedCreateEqual(existing.Entity, doc.Entity) {
				doc.Entity = existing.Entity
				return nil
			}
			return &AskFailure{Kind: "entity_exists", Message: "ask entity already exists with different content", Remedy: "choose a new ID or retry the original normalized request"}
		} else if !os.IsNotExist(err) {
			return err
		}
		return writeAsk(path, doc.Entity, nil, doc.Body)
	})
}

func ensureAsksConfig(missionDir string) error {
	dir := filepath.Join(missionDir, "asks")
	path := filepath.Join(dir, "config.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return atomicWrite(path, []byte("schema: mish.asks/v1\nstates: [open, closed]\noutcomes: [settled, no-action, superseded]\n"))
	} else if err != nil {
		return err
	}
	return validateAsksConfig(dir)
}

func MutateAsk(missionDir, id, ifUpdated string, now time.Time, fn func(*AskDocument, string) *AskFailure) (*AskDocument, error) {
	var out *AskDocument
	if !ValidAskID(id) {
		return nil, &AskFailure{Kind: "invalid_id", Message: "ask ID is invalid", Remedy: "use an ask- UUIDv7 ID"}
	}
	asksDir := filepath.Join(missionDir, "asks")
	if _, err := os.Stat(asksDir); os.IsNotExist(err) {
		return nil, &AskFailure{Kind: "entity_not_found", Message: fmt.Sprintf("ask entity %s was not found", id), Remedy: "list asks and choose an existing ID"}
	} else if err != nil {
		return nil, err
	}
	err := withExistingAsksLock(asksDir, func() error {
		doc, err := ReadAsk(missionDir, id)
		if err != nil {
			return err
		}
		if doc.Entity.Schema != AskSchema {
			return &AskFailure{Kind: "unsupported_schema_write", Message: "unsupported ask schema cannot be mutated", Remedy: "migrate the entity to mish.ask/v1"}
		}
		if failure := ValidateAskDocument(doc, ""); failure != nil {
			return failure
		}
		if ifUpdated == "" {
			return &AskFailure{Kind: "missing_precondition", Message: "if_updated_at is required", Remedy: "read the entity and retry with its updated_at"}
		}
		if doc.Entity.UpdatedAt != ifUpdated {
			return &AskFailure{Kind: "stale_entity", Message: "ask entity changed since it was read", Remedy: "read the latest entity and retry"}
		}
		stamp := strictlyLater(now, doc.Entity.UpdatedAt)
		if failure := fn(doc, stamp); failure != nil {
			return failure
		}
		doc.Entity.UpdatedAt = stamp
		doc.raw["updated_at"] = stamp
		if err := writeAsk(doc.path, doc.Entity, doc.raw, doc.Body); err != nil {
			return err
		}
		out, err = readAskPath(doc.path)
		return err
	})
	return out, err
}

func withExistingAsksLock(dir string, fn func() error) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func strictlyLater(now time.Time, prior string) string {
	now = now.UTC()
	p, err := time.Parse(time.RFC3339Nano, prior)
	if err == nil && !now.After(p) {
		now = p.Add(time.Nanosecond)
	}
	return now.Format(time.RFC3339Nano)
}

func writeAsk(path string, e AskEntity, raw map[string]any, body []byte) error {
	if raw == nil {
		data, err := yaml.Marshal(e)
		if err != nil {
			return err
		}
		raw = map[string]any{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return err
		}
	}
	front, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	data := append([]byte("---\n"), front...)
	data = append(data, []byte("---\n")...)
	data = append(data, body...)
	return atomicWrite(path, data)
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mish-tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizedCreateEqual(a, b AskEntity) bool {
	b.CreatedAt, b.UpdatedAt = a.CreatedAt, a.UpdatedAt
	if a.Blocking != nil && b.Blocking != nil {
		b.Blocking.At = a.Blocking.At
	}
	if len(a.RulingTrail) == 1 && len(b.RulingTrail) == 1 {
		b.RulingTrail[0].At = a.RulingTrail[0].At
	}
	aa, _ := yaml.Marshal(a)
	bb, _ := yaml.Marshal(b)
	return string(aa) == string(bb)
}

func AppendReply(doc *AskDocument, reply Reply) {
	doc.Entity.Replies = append(doc.Entity.Replies, reply)
	appendRaw(doc.raw, "replies", reply)
}
func AppendSettlementRuling(doc *AskDocument, r RulingEntry) {
	doc.Entity.RulingTrail = append(doc.Entity.RulingTrail, r)
	item := map[string]any{"question": r.Question, "options_as_presented": []any{}, "prose": r.Prose, "actor": r.Actor, "at": r.At}
	if framing, ok := doc.raw["framing"].(map[string]any); ok {
		if q, exists := framing["question"]; exists {
			item["question"] = q
		}
		if options, exists := framing["options"]; exists {
			item["options_as_presented"] = options
		}
	}
	if r.Choice != "" {
		item["choice"] = r.Choice
	}
	seq, _ := doc.raw["ruling_trail"].([]any)
	doc.raw["ruling_trail"] = append(seq, item)
}
func AppendTrace(doc *AskDocument, t Trace) {
	doc.Entity.Traces = append(doc.Entity.Traces, t)
	appendRaw(doc.raw, "traces", t)
}
func AppendLink(doc *AskDocument, link TypedRef) {
	doc.Entity.Links = append(doc.Entity.Links, link)
	appendRaw(doc.raw, "links", link)
}
func AppendMember(doc *AskDocument, member string) {
	doc.Entity.Members = append(doc.Entity.Members, member)
	seq, _ := doc.raw["members"].([]any)
	doc.raw["members"] = append(seq, member)
}
func SetRaw(doc *AskDocument, key string, value any) { doc.raw[key] = value }
func SetAnchor(doc *AskDocument, value TypedRef) {
	m, _ := doc.raw["anchor"].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m["type"], m["ref"] = value.Type, value.Ref
	doc.raw["anchor"] = m
}
func appendRaw(raw map[string]any, key string, value any) {
	data, _ := yaml.Marshal(value)
	var item any
	_ = yaml.Unmarshal(data, &item)
	seq, _ := raw[key].([]any)
	raw[key] = append(seq, item)
}
