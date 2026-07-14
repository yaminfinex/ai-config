package surface

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Raw-view caps. Lines are display-truncated with honest byte counts; the
// mirror keeps the full bytes. The session-level budget bounds what one
// request can hold in memory — pages render buffered, so without it an
// adversarially large mirrored session (many large lines) would OOM the
// store instead of degrading.
const (
	rawLineDisplayBytes   = 1 << 20
	maxRawLines           = 10000
	rawDisplayBudgetBytes = 8 << 20
)

// rawPage is the template model for the raw-JSONL fallback (R16, S10): the
// mirror's lines, file by file in first-ingest order, no interpretation.
type rawPage struct {
	Session SessionSummary
	Reason  string
	Files   []rawFile
	// BudgetExhausted flags that the display budget cut the page short;
	// the remaining bytes stay in the mirror, honestly noticed.
	BudgetExhausted bool
}

type rawFile struct {
	FileUUID   string
	Generation int
	Err        string // per-file read failure, shown honestly
	Lines      []rawLine
	More       bool // line-count cap hit
}

type rawLine struct {
	Text       string
	Truncated  bool
	TotalBytes int64
}

// serveRawFallback renders mirror lines for every file of the session. It is
// the never-500 backstop for a mirrored session: per-file read errors render
// as notices, and only a fully failed render drops to the degraded page.
func (s *Server) serveRawFallback(w http.ResponseWriter, r *http.Request, sum SessionSummary, reason string) {
	page := rawPage{Session: sum, Reason: reason}
	budget := int64(rawDisplayBudgetBytes)
	// Per-file mirror failures repeat across a many-file session, so they
	// aggregate to one journal line per error class per request.
	openFails, readFails := map[string]int{}, map[string]int{}
	for _, ref := range sessionFilesFirstIngest(sum) {
		if budget <= 0 {
			page.BudgetExhausted = true
			break
		}
		rf := rawFile{FileUUID: ref.FileUUID, Generation: ref.Generation}
		rc, err := s.store.MirrorFile(r.Context(), sum.Tool, ref.WireSessionID, ref.FileUUID, ref.Generation)
		if err != nil {
			openFails[errClass(err)]++
			rf.Err = "mirror file unreadable"
			page.Files = append(page.Files, rf)
			continue
		}
		rf.Lines, rf.More, err = readRawLines(rc, &budget)
		rc.Close()
		if rf.More && budget <= 0 {
			page.BudgetExhausted = true
		}
		if err != nil {
			readFails[errClass(err)]++
			rf.Err = "mirror read failed mid-file; partial lines shown"
		}
		page.Files = append(page.Files, rf)
	}
	for class, n := range openFails {
		s.log.Warn("surface: raw fallback mirror open failed", "tool", string(sum.Tool), "error_class", class, "files", n)
	}
	for class, n := range readFails {
		s.log.Warn("surface: raw fallback mirror read failed", "tool", string(sum.Tool), "error_class", class, "files", n)
	}
	if err := s.render(w, s.rawTmpl, "raw.html", page); err != nil {
		s.log.Warn("surface: page render failed", "route", "/s/*", "error_class", errClass(err))
		s.writeDegraded(w, "raw fallback render failed")
	}
}

// sessionFilesFirstIngest returns the session's files in first-ingest order
// (already the Files contract; sorted defensively here because ordering is
// what the fully-quarantined AC hangs on).
func sessionFilesFirstIngest(sum SessionSummary) []FileRef {
	files := append([]FileRef(nil), sum.Files...)
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && files[j].FirstIngestAt.Before(files[j-1].FirstIngestAt); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
	return files
}

// readRawLines scans mirrored bytes into display lines, cutting oversized
// lines at rawLineDisplayBytes without buffering the remainder and charging
// every displayed byte against the caller's budget. A trailing partial line
// (no newline yet) renders like any other bytes — the mirror is
// byte-faithful and so is this view.
func readRawLines(rc io.Reader, budget *int64) (lines []rawLine, more bool, err error) {
	br := bufio.NewReaderSize(rc, 64<<10)
	for len(lines) < maxRawLines && *budget > 0 {
		line, readErr := readCappedLine(br)
		if line != nil {
			lines = append(lines, *line)
			*budget -= int64(len(line.Text))
		}
		if readErr == io.EOF {
			return lines, false, nil
		}
		if readErr != nil {
			return lines, false, readErr
		}
	}
	// Line cap or budget hit: report there is more without reading it.
	if _, err := br.Peek(1); err == nil {
		more = true
	}
	return lines, more, nil
}

func readCappedLine(br *bufio.Reader) (*rawLine, error) {
	var (
		text  []byte
		total int64
		cut   bool
	)
	for {
		chunk, err := br.ReadSlice('\n')
		total += int64(len(chunk))
		if !cut {
			room := rawLineDisplayBytes - len(text)
			if len(chunk) > room {
				chunk, cut = chunk[:room], true
			}
			text = append(text, chunk...)
		}
		switch err {
		case nil:
			n := len(text)
			for n > 0 && (text[n-1] == '\n' || text[n-1] == '\r') {
				n--
			}
			return &rawLine{Text: string(text[:n]), Truncated: cut, TotalBytes: total}, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if total == 0 {
				return nil, io.EOF
			}
			return &rawLine{Text: string(text), Truncated: cut, TotalBytes: total}, io.EOF
		default:
			if total == 0 {
				return nil, err
			}
			return &rawLine{Text: string(text), Truncated: cut, TotalBytes: total}, err
		}
	}
}

// --- template format helpers ---

// fmtTime accepts time.Time and *time.Time; nil (no parsed timestamp)
// renders as an honest dash.
func fmtTime(v any) string {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format("2006-01-02 15:04:05") + " UTC"
	case *time.Time:
		if t != nil {
			return t.UTC().Format("2006-01-02 15:04:05") + " UTC"
		}
	}
	return "—"
}

func fmtAge(now time.Time, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "in the future"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func fmtSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
