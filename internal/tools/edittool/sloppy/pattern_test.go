package sloppy

import (
	"strings"
	"testing"
)

func TestScanPatternGapBoundedness(t *testing.T) {
	p, err := scanPattern("a …b\n…\nc")
	if err != nil {
		t.Fatalf("scanPattern() error = %v", err)
	}
	var gaps []PatternToken
	for _, tok := range p.Tokens {
		if tok.Kind == PatternTokenGap {
			gaps = append(gaps, tok)
		}
	}
	if len(gaps) != 2 {
		t.Fatalf("gaps = %+v, want two", gaps)
	}
	if !gaps[0].LineBounded {
		t.Error("mid-line gap must be line-bounded")
	}
	if gaps[1].LineBounded {
		t.Error("line-end gap may span lines")
	}
}

func TestScanPatternDropsEdgeGaps(t *testing.T) {
	p, err := scanPattern("…\nfoo\n…")
	if err != nil {
		t.Fatalf("scanPattern() error = %v", err)
	}
	if !p.EdgeGaps.Leading || !p.EdgeGaps.Trailing {
		t.Errorf("edge gaps = %+v, want both", p.EdgeGaps)
	}
	want := PatternToken{Kind: PatternTokenLiteral, Text: "foo", Start: 4, End: 7}
	if len(p.Tokens) != 1 || p.Tokens[0] != want {
		t.Errorf("tokens = %+v, want %+v (edge gaps take their joining newline)", p.Tokens, want)
	}
}

func TestScanPatternRenumbersAfterEdgeDrop(t *testing.T) {
	p, err := scanPattern("…a…b…")
	if err != nil {
		t.Fatalf("scanPattern() error = %v", err)
	}
	if !p.EdgeGaps.Leading || !p.EdgeGaps.Trailing {
		t.Fatalf("edge gaps = %+v, want both", p.EdgeGaps)
	}
	if len(p.Tokens) != 3 || p.Tokens[1].Kind != PatternTokenGap || p.Tokens[1].Capture != 0 {
		t.Errorf("tokens = %+v, want the surviving gap numbered 0", p.Tokens)
	}
}

func TestScanPatternSelectionErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "stray close", body: "a ⟫ b", want: "stray"},
		{name: "unterminated", body: "a ⟪b│c", want: "unterminated"},
		{name: "nested open", body: "a ⟪b⟪c│d⟫", want: "nested"},
		{name: "missing divider", body: "a ⟪b⟫ c", want: "divider"},
		{name: "multiple dividers", body: "a ⟪b│c│d⟫", want: "multiple"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scanPattern(tt.body)
			if err == nil {
				t.Fatal("scanPattern() error = nil, want failure")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Errorf("error = %q, want substring %q", got, tt.want)
			}
		})
	}
}
