package sloppy

import (
	"errors"
	"strings"
	"testing"
)

func mustParse(t *testing.T, input string) []Section {
	t.Helper()
	sections, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return sections
}

func TestParseBasicReplace(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT a.ts\n*** SM:FIND\nconst timeout = 1000;\n*** SM:PUT\nconst timeout = 5000;\n")
	if len(sections) != 1 || sections[0].Path != "a.ts" || len(sections[0].Ops) != 1 {
		t.Fatalf("sections = %+v, want one a.ts section with one op", sections)
	}
	op := sections[0].Ops[0]
	if op.Number != 1 || op.Line != 2 || op.All || op.Desired {
		t.Errorf("op header fields = %+v, want number 1 line 2", op)
	}
	if op.Rewrite != (Rewrite{Kind: RewriteReplace, Text: "const timeout = 5000;"}) {
		t.Errorf("rewrite = %+v", op.Rewrite)
	}
	if op.Pattern.Body != "const timeout = 1000;" {
		t.Errorf("pattern body = %q", op.Pattern.Body)
	}
	if len(op.Pattern.Tokens) != 1 || op.Pattern.Tokens[0].Kind != PatternTokenLiteral ||
		op.Pattern.Tokens[0].Text != "const timeout = 1000;" {
		t.Errorf("tokens = %+v", op.Pattern.Tokens)
	}
}

func TestParseEmptyPutDeletes(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT a.ts\n*** SM:FIND\ndebugLog(request);\n*** SM:PUT\n")
	op := sections[0].Ops[0]
	if op.Rewrite.Kind != RewriteReplace || op.Rewrite.Text != "" {
		t.Errorf("rewrite = %+v, want empty SM:PUT delete", op.Rewrite)
	}
}

func TestParseAllFlag(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT a.ts all\n*** SM:FIND\nlogger.debug(\n*** SM:PUT\nlogger.trace(\n")
	if !sections[0].Ops[0].All {
		t.Error("op must inherit the SM:EDIT all flag")
	}
	sections = mustParse(t, "*** SM:EDIT a.ts all\n*** SM:FIND\nx\n*** SM:PUT\ny\n*** SM:EDIT\n*** SM:FIND\nz\n*** SM:PUT\nw\n")
	ops := sections[0].Ops
	if len(ops) != 2 {
		t.Fatalf("ops = %+v, want two", ops)
	}
	if !ops[0].All || ops[1].All {
		t.Errorf("all flags = %v, %v; want true, false after a bare SM:EDIT", ops[0].All, ops[1].All)
	}
}

func TestParseCoalescesSectionsInFirstSeenOrder(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n"+
		"*** SM:EDIT b.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n"+
		"*** SM:EDIT a.ts\n*** SM:FIND\nz\n*** SM:PUT\nw\n")
	if len(sections) != 2 || sections[0].Path != "a.ts" || sections[1].Path != "b.ts" {
		t.Fatalf("sections = %+v, want a.ts then b.ts", sections)
	}
	if len(sections[0].Ops) != 2 || sections[0].Ops[1].Pattern.Body != "z" {
		t.Errorf("a.ts ops = %+v, want both ops coalesced", sections[0].Ops)
	}
}

func TestParseAfterInsert(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT src/retry.ts\n*** SM:FIND\n\tlimit: number;\n*** SM:AFTER\n\tdelayMs: number;\n")
	op := sections[0].Ops[0]
	if op.Rewrite != (Rewrite{Kind: RewriteInsert, Text: "\tdelayMs: number;"}) {
		t.Errorf("rewrite = %+v, want SM:AFTER insert", op.Rewrite)
	}
}

func TestParseGapAndSelectionTokens(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT a.ts\n*** SM:FIND\ntimeout = …⟪1000│5000⟫…\nrun(timeout)\n")
	op := sections[0].Ops[0]
	if op.Rewrite.Kind != RewriteInline {
		t.Fatalf("rewrite kind = %v, want inline selection", op.Rewrite.Kind)
	}
	body := "timeout = …⟪1000│5000⟫…\nrun(timeout)"
	if op.Pattern.Body != body {
		t.Fatalf("body = %q, want %q", op.Pattern.Body, body)
	}
	want := []PatternToken{
		{Kind: PatternTokenLiteral, Text: "timeout = ", Start: 0, End: 10},
		{Kind: PatternTokenGap, Capture: 0, LineBounded: true, Start: 10, End: 13},
		{Kind: PatternTokenLiteral, Text: "1000", Start: 16, End: 20},
		{Kind: PatternTokenGap, Capture: 1, Start: 30, End: 33},
		{Kind: PatternTokenLiteral, Text: "\nrun(timeout)", Start: 33, End: 46},
	}
	if len(op.Pattern.Tokens) != len(want) {
		t.Fatalf("tokens = %+v, want %+v", op.Pattern.Tokens, want)
	}
	for i := range want {
		got := op.Pattern.Tokens[i]
		if got != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, got, want[i])
		}
		if got.Kind == PatternTokenLiteral && got.Text != op.Pattern.Body[got.Start:got.End] {
			t.Errorf("literal token %d text %q does not match body[%d:%d]", i, got.Text, got.Start, got.End)
		}
	}
	wantSel := Selection{Old: "1000", New: "5000", Start: 13, End: 30}
	if len(op.Pattern.Selections) != 1 || op.Pattern.Selections[0] != wantSel {
		t.Errorf("selections = %+v, want %+v", op.Pattern.Selections, wantSel)
	}
}

func TestParseDropsReadOutputNoiseAndNumbering(t *testing.T) {
	input := "*** SM:EDIT a.ts\n*** SM:FIND\n" +
		"1| const timeout = 1000;\n" +
		"[Showing lines 1-3 of 3]\n" +
		"2-3: …\n" +
		"2| run(timeout);\n" +
		"[2 more lines in a.ts. use read to continue]\n" +
		"*** SM:PUT\n1| const timeout = 5000;\n2| run(timeout);\n"
	sections := mustParse(t, input)
	op := sections[0].Ops[0]
	if op.Pattern.Body != "const timeout = 1000;\nrun(timeout);" {
		t.Errorf("find body = %q", op.Pattern.Body)
	}
	if op.Rewrite.Text != "const timeout = 5000;\nrun(timeout);" {
		t.Errorf("put body = %q", op.Rewrite.Text)
	}
}

func TestParseDesiredOperation(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT a.ts\n*** SM:PUT\nfinal content\n")
	op := sections[0].Ops[0]
	if !op.Desired || op.Rewrite.Text != "final content" {
		t.Errorf("op = %+v, want desired-state op", op)
	}
}

func TestParseQuotedPath(t *testing.T) {
	sections := mustParse(t, "*** SM:EDIT \"my file all.ts\" all\n*** SM:FIND\nx\n*** SM:PUT\ny\n")
	if sections[0].Path != "my file all.ts" || !sections[0].Ops[0].All {
		t.Errorf("section = %+v, want quoted path with all", sections[0])
	}
}

func TestParseStripsOuterFence(t *testing.T) {
	sections := mustParse(t, "```text\n*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n```\n")
	if len(sections) != 1 || sections[0].Ops[0].Pattern.Body != "x" {
		t.Errorf("sections = %+v, want fenced payload parsed", sections)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		op    int
		want  string
	}{
		{
			name:  "prose without target",
			input: "just some prose\n",
			want:  "missing file target",
		},
		{
			name:  "bare SM:EDIT without path",
			input: "*** SM:EDIT\n*** SM:FIND\nx\n*** SM:PUT\ny\n",
			want:  "missing file target",
		},
		{
			name:  "find without action",
			input: "*** SM:EDIT a.ts\n*** SM:FIND\nx\n",
			op:    1,
			want:  "missing *** SM:PUT or *** SM:AFTER action",
		},
		{
			name:  "after without anchor",
			input: "*** SM:EDIT a.ts\n*** SM:AFTER\ny\n",
			op:    1,
			want:  "*** SM:AFTER requires a *** SM:FIND anchor",
		},
		{
			name:  "duplicate action header",
			input: "*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n*** SM:PUT\nz\n",
			op:    1,
			want:  "duplicate SM:PUT header",
		},
		{
			name:  "stray close marker",
			input: "*** SM:EDIT a.ts\n*** SM:FIND\na ⟫ b\n*** SM:PUT\nc\n",
			op:    1,
			want:  "stray",
		},
		{
			name:  "unterminated selection",
			input: "*** SM:EDIT a.ts\n*** SM:FIND\na ⟪b│c\n*** SM:PUT\nd\n",
			op:    1,
			want:  "unterminated",
		},
		{
			name:  "selection without divider",
			input: "*** SM:EDIT a.ts\n*** SM:FIND\na ⟪b⟫ c\n*** SM:PUT\nd\n",
			op:    1,
			want:  "divider",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			var perr *ParseError
			if !errors.As(err, &perr) {
				t.Fatalf("error = %v (%T), want *ParseError", err, err)
			}
			if !strings.Contains(perr.Msg, tt.want) {
				t.Errorf("message = %q, want substring %q", perr.Msg, tt.want)
			}
			if perr.Op != tt.op {
				t.Errorf("op = %d, want %d", perr.Op, tt.op)
			}
			if tt.op > 0 && perr.Line == 0 {
				t.Error("line must be set for operation errors")
			}
		})
	}
}
