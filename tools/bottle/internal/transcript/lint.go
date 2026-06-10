package transcript

import (
	"fmt"
	"io"
)

// Lint checks a transcript for dangling uuid references: every non-null
// parentUuid and logicalParentUuid, every last-prompt/summary leafUuid, and
// every file-history-snapshot messageId must resolve to a tree node present
// in the stream. The truncation tests run it on every output; U5/U6 can run
// it on materialized seeds as cheap insurance.
//
// compactMetadata uuid lists are deliberately not checked: pristine Claude
// Code files already reference rewound-away uuids there (U1 census), so they
// can never be a truncation-correctness signal.
func Lint(r io.Reader) error {
	info, err := Index(r)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(info.Entries))
	for i := range info.Entries {
		e := &info.Entries[i]
		if e.Class() == ClassTreeNode && e.UUID != "" {
			present[e.UUID] = true
		}
	}
	check := func(e *Entry, field, ref string) error {
		if ref != "" && !present[ref] {
			return fmt.Errorf("line %d: %s %s %q references uuid absent from transcript", e.Line, e.Type, field, ref)
		}
		return nil
	}
	for i := range info.Entries {
		e := &info.Entries[i]
		if err := check(e, "parentUuid", e.ParentUUID); err != nil {
			return err
		}
		if err := check(e, "logicalParentUuid", e.LogicalParentUUID); err != nil {
			return err
		}
		switch e.Type {
		case "last-prompt", "summary":
			if err := check(e, "leafUuid", e.LeafUUID); err != nil {
				return err
			}
		case "file-history-snapshot":
			if err := check(e, "messageId", e.MessageID); err != nil {
				return err
			}
		}
	}
	return nil
}
