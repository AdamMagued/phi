package sloppy

import "testing"

func collect(t *testing.T, input string) []Token {
	t.Helper()
	var toks []Token
	lx := NewLexer(input)
	for lx.Next() {
		toks = append(toks, lx.Token())
	}
	return toks
}

func TestLexerRecognizesHeaders(t *testing.T) {
	toks := collect(t, "*** SM:EDIT src/a.ts all\n*** SM:FIND\nx\n*** SM:PUT\ny\n*** SM:AFTER\nz\n")
	want := []Token{
		{Kind: TokenEdit, Line: 1, Text: "src/a.ts all", Path: "src/a.ts", All: true},
		{Kind: TokenFind, Line: 2},
		{Kind: TokenText, Line: 3, Text: "x"},
		{Kind: TokenPut, Line: 4},
		{Kind: TokenText, Line: 5, Text: "y"},
		{Kind: TokenAfter, Line: 6},
		{Kind: TokenText, Line: 7, Text: "z"},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, toks[i], want[i])
		}
	}
}

func TestLexerEditArgumentForms(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Token
		text bool // expect the line to fall back to body text
	}{
		{name: "bare continues section", line: "*** SM:EDIT", want: Token{Kind: TokenEdit}},
		{name: "all without path", line: "*** SM:EDIT all", want: Token{Kind: TokenEdit, All: true}},
		{name: "path only", line: "*** SM:EDIT a.ts", want: Token{Kind: TokenEdit, Path: "a.ts"}},
		{name: "path with all", line: "*** SM:EDIT a.ts all", want: Token{Kind: TokenEdit, Path: "a.ts", All: true}},
		{name: "path ending in all word", line: "*** SM:EDIT call.ts", want: Token{Kind: TokenEdit, Path: "call.ts"}},
		{
			name: "quoted path with all",
			line: `*** SM:EDIT "my file all.ts" all`,
			want: Token{Kind: TokenEdit, Path: "my file all.ts", All: true},
		},
		{name: "empty quoted path is text", line: `*** SM:EDIT ""`, text: true},
		{name: "malformed quote is text", line: `*** SM:EDIT "unterminated`, text: true},
		{name: "stray argument is text", line: "*** SM:PUT oops", text: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks := collect(t, tt.line+"\n")
			if tt.text {
				if len(toks) != 1 || toks[0].Kind != TokenText || toks[0].Text != tt.line {
					t.Fatalf("got %+v, want body text %q", toks, tt.line)
				}
				return
			}
			if len(toks) != 1 {
				t.Fatalf("got %d tokens, want 1: %+v", len(toks), toks)
			}
			want := tt.want
			want.Line, want.Text = 1, toks[0].Text
			if toks[0] != want {
				t.Errorf("got %+v, want %+v", toks[0], want)
			}
		})
	}
}

func TestLexerStripsForeignPatchEnvelope(t *testing.T) {
	input := "*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n" +
		"*** Begin Patch\n*** Update File: b.ts\nnoise\n*** End Patch\ntrailer prose\n" +
		"*** SM:EDIT c.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n"
	toks := collect(t, input)
	var got []Token
	for _, tok := range toks {
		if tok.Kind == TokenText {
			got = append(got, tok)
		}
	}
	// Envelope lines vanish and the end sentinel skips the trailer, but body
	// content between envelope lines passes through untouched.
	want := []string{"x", "y", "noise", "x", "y"}
	if len(got) != len(want) {
		t.Fatalf("body lines = %+v, want %v", got, want)
	}
	for i := range want {
		if got[i].Text != want[i] {
			t.Errorf("body line %d = %q, want %q", i, got[i].Text, want[i])
		}
	}
}

func TestLexerKeepsAfterBodiesRaw(t *testing.T) {
	input := "*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:AFTER\n*** Begin Patch\nliteral\n"
	toks := collect(t, input)
	want := []Token{
		{Kind: TokenEdit, Line: 1, Path: "a.ts", Text: "a.ts"},
		{Kind: TokenFind, Line: 2},
		{Kind: TokenText, Line: 3, Text: "x"},
		{Kind: TokenAfter, Line: 4},
		{Kind: TokenText, Line: 5, Text: "*** Begin Patch"},
		{Kind: TokenText, Line: 6, Text: "literal"},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, toks[i], want[i])
		}
	}
}

func TestLexerMergesSplitEnvelopeSentinel(t *testing.T) {
	input := "*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n***\nEnd of patch\nstray\n"
	toks := collect(t, input)
	for _, tok := range toks {
		if tok.Kind == TokenText && tok.Text == "stray" {
			t.Fatal("text after split end sentinel must be skipped")
		}
	}
	if len(toks) != 5 {
		t.Fatalf("got %d tokens, want 5: %+v", len(toks), toks)
	}
}
