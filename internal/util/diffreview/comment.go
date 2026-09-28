package diffreview

import (
	"sort"
	"strconv"
	"strings"
)

// Side identifies which side of a unified diff a comment anchors to.
type Side string

const (
	SideLeft  Side = "LEFT"
	SideRight Side = "RIGHT"
)

// Anchor locates one visible diff line for a comment.
type Anchor struct {
	Path     string
	Line     int
	Side     Side
	CommitID string
}

// CommentDraft is one review note on a single line. Drafts live only in the
// pane's memory; nothing is written to disk.
type CommentDraft struct {
	ID       string
	Path     string
	Body     string
	CommitID string
	Line     int
	Side     Side
}

// CommentIndex maps visible drafts to the rows that render them.
type CommentIndex struct {
	entries []commentIndexEntry
}

type commentIndexEntry struct {
	draft CommentDraft
	row   int
}

// BuildCommentIndex indexes drafts that resolve against the current visible rows.
func BuildCommentIndex(rows []Row, drafts []CommentDraft) CommentIndex {
	entries := make([]commentIndexEntry, 0, len(drafts))
	for _, draft := range drafts {
		for rowIndex, row := range rows {
			if DraftEndsAt(draft, row.Review) {
				entries = append(entries, commentIndexEntry{draft: draft, row: rowIndex})
				break
			}
		}
	}
	return CommentIndex{entries: entries}
}

// DraftsForRow returns comments that render under row.
func (idx CommentIndex) DraftsForRow(row int) []CommentDraft {
	drafts := make([]CommentDraft, 0, 1)
	for _, entry := range idx.entries {
		if entry.row == row {
			drafts = append(drafts, entry.draft)
		}
	}
	if len(drafts) == 0 {
		return nil
	}
	return drafts
}

// TargetRows returns unique, sorted document rows that have comments.
func (idx CommentIndex) TargetRows() []int {
	seen := make(map[int]bool, len(idx.entries))
	targets := make([]int, 0, len(idx.entries))
	for _, entry := range idx.entries {
		if seen[entry.row] {
			continue
		}
		seen[entry.row] = true
		targets = append(targets, entry.row)
	}
	sort.Ints(targets)
	return targets
}

// AnchorValid reports whether an anchor can hold a comment.
func AnchorValid(anchor Anchor) bool {
	return anchor.Path != "" && anchor.Line > 0 && anchor.Side != ""
}

// DraftFromAnchor builds a new empty comment at anchor.
func DraftFromAnchor(anchor Anchor) CommentDraft {
	return CommentDraft{
		Path:     anchor.Path,
		Line:     anchor.Line,
		Side:     anchor.Side,
		CommitID: anchor.CommitID,
	}
}

// DraftEndsAt reports whether draft's end anchor matches.
func DraftEndsAt(draft CommentDraft, anchor Anchor) bool {
	return draft.Path == anchor.Path &&
		draft.Line == anchor.Line &&
		draft.Side == anchor.Side &&
		draft.CommitID == anchor.CommitID
}

// SameTarget reports whether two drafts point at the same line.
func SameTarget(a, b CommentDraft) bool {
	return a.Path == b.Path &&
		a.Line == b.Line &&
		a.Side == b.Side &&
		a.CommitID == b.CommitID
}

// FormatPrompt turns drafts into a prompt the agent can act on.
func FormatPrompt(drafts []CommentDraft) string {
	var b strings.Builder
	b.WriteString("Address these review comments on the current diff. Cite paths and lines.\n")
	for _, d := range drafts {
		body := strings.TrimSpace(d.Body)
		if body == "" {
			continue
		}
		side := string(d.Side)
		if side == "" {
			side = "RIGHT"
		}
		b.WriteString("\n### ")
		b.WriteString(d.Path)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(d.Line))
		b.WriteString(" (")
		b.WriteString(side)
		b.WriteString(")\n")
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String()
}
