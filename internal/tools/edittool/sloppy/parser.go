package sloppy

import (
	"fmt"
	"strings"
)

const errMissingTarget = "missing file target: start the payload with *** SM:EDIT relative/path"

// ParseError describes why a payload failed to parse.
type ParseError struct {
	Op   int // 1-based operation number, 0 when not attributable
	Line int // 1-based input line
	Msg  string
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	switch {
	case e.Op > 0:
		return fmt.Sprintf("operation %d at line %d: %s", e.Op, e.Line, e.Msg)
	case e.Line > 0:
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	default:
		return e.Msg
	}
}

// Parse compiles a sloppy payload into ordered file sections. Sections keep
// first-seen path order; later headers for the same path extend that section.
func Parse(input string) ([]Section, error) {
	p := &parser{lx: NewLexer(trimOuterFence(input)), byPath: make(map[string]*Section)}
	return p.run()
}

type bodyState uint8

const (
	stateIdle bodyState = iota
	stateFind
	statePut
)

type parser struct {
	lx      *Lexer
	order   []*Section
	byPath  map[string]*Section
	target  *Section
	all     bool
	state   bodyState
	find    []string
	put     []string
	putKind RewriteKind
	havePut bool
	findLn  int
	putLn   int
	ops     int
	started bool
}

func (p *parser) run() ([]Section, error) {
	for p.lx.Next() {
		tok := p.lx.Token()
		if !p.started {
			if tok.Kind == TokenText && strings.TrimSpace(tok.Text) == "" {
				continue // leading blank lines
			}
			if tok.Kind != TokenEdit || tok.Path == "" {
				return nil, &ParseError{Line: tok.Line, Msg: errMissingTarget}
			}
		}
		var err error
		switch tok.Kind {
		case TokenEdit:
			if err = p.flush(); err == nil {
				p.started = true
				p.all = tok.All
				if tok.Path != "" {
					p.target = p.section(tok.Path)
				}
			}
		case TokenFind:
			if err = p.flush(); err == nil {
				p.state = stateFind
				p.findLn = tok.Line
			}
		case TokenPut, TokenAfter:
			if p.havePut {
				err = &ParseError{
					Op:   p.ops + 1,
					Line: tok.Line,
					Msg:  "duplicate " + tok.Kind.String() + " header without a *** SM:FIND anchor",
				}
			} else {
				p.havePut = true
				p.putKind = RewriteReplace
				if tok.Kind == TokenAfter {
					p.putKind = RewriteInsert
				}
				p.putLn = tok.Line
				p.state = statePut
			}
		case TokenText:
			p.append(tok)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := p.flush(); err != nil {
		return nil, err
	}
	sections := make([]Section, len(p.order))
	for i, s := range p.order {
		sections[i] = *s
	}
	return sections, nil
}

// append routes a body line to the open FIND or action buffer, dropping
// read-output chrome such as "[Showing lines 1-20 of 80]".
func (p *parser) append(tok Token) {
	line := tok.Text
	if isBodyNoise(strings.TrimSpace(line)) {
		return
	}
	switch p.state {
	case statePut:
		p.put = append(p.put, line)
	case stateFind:
		p.find = append(p.find, line)
	default:
		p.state = stateFind
		p.findLn = tok.Line
		p.find = append(p.find, line)
	}
}

// flush closes the open operation, if any, at the next header or end of input.
func (p *parser) flush() error {
	find := joinBody(p.find)
	put := joinAction(p.put)
	findLn, putLn := p.findLn, p.putLn
	kind, havePut := p.putKind, p.havePut
	p.find, p.put = p.find[:0], p.put[:0]
	p.havePut = false
	p.state = stateIdle
	p.findLn, p.putLn = 0, 0

	op := Operation{Number: p.ops + 1, All: p.all}
	switch {
	case find == "" && !havePut:
		return nil
	case find == "":
		if kind == RewriteInsert {
			return &ParseError{Op: op.Number, Line: putLn, Msg: "*** SM:AFTER requires a *** SM:FIND anchor"}
		}
		if put == "" {
			return nil
		}
		// An anchor-less PUT asserts desired file content.
		op.Desired = true
		op.Line = putLn
		op.Rewrite = Rewrite{Kind: RewriteReplace, Text: put}
	default:
		pattern, err := scanPattern(find)
		if err != nil {
			return &ParseError{Op: op.Number, Line: findLn, Msg: err.Error()}
		}
		op.Pattern = pattern
		op.Line = findLn
		switch {
		case havePut:
			op.Rewrite = Rewrite{Kind: kind, Text: put}
		case len(pattern.Selections) > 0:
			op.Rewrite = Rewrite{Kind: RewriteInline}
		default:
			return &ParseError{Op: op.Number, Line: findLn, Msg: "missing *** SM:PUT or *** SM:AFTER action"}
		}
	}
	p.ops++
	p.target.Ops = append(p.target.Ops, op)
	return nil
}

// section returns the section for path, creating it on first use.
func (p *parser) section(path string) *Section {
	if s, ok := p.byPath[path]; ok {
		return s
	}
	s := &Section{Path: path}
	p.byPath[path] = s
	p.order = append(p.order, s)
	return s
}

// joinBody trims blank edges, strips uniform line numbering, and joins the
// body lines. A pattern quotes current text: blank lines pasted around it are
// noise, not content.
func joinBody(lines []string) string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return joinLines(lines[start:end])
}

// joinAction joins a rewrite body, keeping blank edges: in *** SM:PUT and
// *** SM:AFTER an authored blank line is content.
func joinAction(lines []string) string {
	return joinLines(lines)
}

// joinLines strips uniform read-output line numbering and joins body lines.
func joinLines(body []string) string {
	if len(body) == 0 {
		return ""
	}
	if allNumbered(body) {
		var b strings.Builder
		for i, line := range body {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(stripNumbered(line))
		}
		return b.String()
	}
	return strings.Join(body, "\n")
}

// allNumbered reports whether every line carries a read-output line number.
func allNumbered(lines []string) bool {
	for _, line := range lines {
		if _, ok := splitNumbered(line); !ok {
			return false
		}
	}
	return len(lines) > 0
}

// stripNumbered removes the read-output line-number prefix from line.
func stripNumbered(line string) string {
	rest, ok := splitNumbered(line)
	if !ok {
		return line
	}
	return rest
}

// splitNumbered splits "12| code" or "12: code" into its content, reporting
// whether the line carried a numeric prefix.
func splitNumbered(line string) (string, bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	digits := i
	for i < len(line) && '0' <= line[i] && line[i] <= '9' {
		i++
	}
	if i == digits || i == len(line) {
		return "", false
	}
	if line[i] != '|' && line[i] != ':' {
		return "", false
	}
	i++
	if i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[i:], true
}

// isBodyNoise reports whether a body line is read-output chrome rather than
// content: "[Showing lines 1-20 of 80]", "[3 more lines in a.ts. use read to
// continue]", or an elided-line marker such as "12-14: …".
func isBodyNoise(line string) bool {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		inner := line[1 : len(line)-1]
		switch {
		case hasPrefixFold(inner, "showing lines"):
			return true
		case strings.Contains(inner, " more line") &&
			strings.Contains(inner, " in ") &&
			strings.Contains(inner, ". use ") &&
			hasSuffixFold(inner, "to continue"):
			return true
		}
	}
	return isElidedMarker(line)
}

// isElidedMarker reports whether line is a read-output elision marker such as
// "12: …" or "12-14: ...".
func isElidedMarker(line string) bool {
	i := 0
	digits := func() bool {
		start := i
		for i < len(line) && '0' <= line[i] && line[i] <= '9' {
			i++
		}
		return i > start
	}
	if !digits() {
		return false
	}
	if i < len(line) && line[i] == '-' {
		i++
		if !digits() {
			return false
		}
	}
	if i == len(line) || line[i] != ':' {
		return false
	}
	rest := strings.TrimSpace(line[i+1:])
	return rest == gapMarker || rest == "..."
}

// hasSuffixFold reports whether s ends with suffix under ASCII
// case-insensitive matching without allocating.
func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

// trimOuterFence removes a markdown fence wrapping the whole payload.
func trimOuterFence(input string) string {
	first := input
	rest := ""
	if before, after, ok := strings.Cut(input, "\n"); ok {
		first, rest = before, after
	}
	if !isOuterFence(strings.TrimSpace(first)) {
		return input
	}
	inner := strings.TrimRight(rest, " \t\r\n")
	return strings.TrimRight(strings.TrimSuffix(inner, "```"), " \t\r\n")
}

// isOuterFence reports whether line opens a bare markdown fence, with or
// without one of the language tags models commonly use for payloads.
func isOuterFence(line string) bool {
	if !strings.HasPrefix(line, "```") {
		return false
	}
	tag := strings.TrimSpace(line[3:])
	switch strings.ToLower(tag) {
	case "", "text", "xml", "html", "typescript", "ts", "tsx", "javascript", "js":
		return true
	}
	return false
}
