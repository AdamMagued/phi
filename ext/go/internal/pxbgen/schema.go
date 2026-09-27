package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// message is one wire message in schema declaration order.
type message struct {
	name   string // struct name, e.g. ToolResultMsg
	pub    string // function base name, e.g. ToolResult (//pxb:pub)
	fields []field
}

// field is one tagged struct field.
type field struct {
	name string
	typ  string
	tag  uint16
	opt  bool // omitted on the wire when zero
	kind kind
}

func parseSchema(path string) ([]message, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var msgs []message
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			msg, ok, err := parseMessage(gd, ts, st)
			if err != nil {
				return nil, err
			}
			if ok {
				msgs = append(msgs, msg)
			}
		}
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("%s: no tagged structs", path)
	}
	return msgs, nil
}

// parseMessage returns ok=false for structs without any pxb tag, so schema.go
// may hold helper structs next to the wire messages.
func parseMessage(gd *ast.GenDecl, ts *ast.TypeSpec, st *ast.StructType) (message, bool, error) {
	if !hasTags(st) {
		return message{}, false, nil
	}
	msg := message{name: ts.Name.Name, pub: pubName(docOf(gd, ts), ts.Name.Name)}
	seen := make(map[uint16]string, len(st.Fields.List))
	for _, f := range st.Fields.List {
		name, lit, tagged, err := fieldName(ts.Name.Name, f)
		if err != nil {
			return message{}, false, err
		}
		if !tagged {
			return message{}, false, fmt.Errorf("%s.%s: message fields need a pxb tag", ts.Name.Name, name)
		}
		tag, opt, err := parseTag(lit)
		if err != nil {
			return message{}, false, fmt.Errorf("%s.%s: %w", ts.Name.Name, name, err)
		}
		if prev, dup := seen[tag]; dup {
			return message{}, false, fmt.Errorf("%s: tag %d used by %s and %s", ts.Name.Name, tag, prev, name)
		}
		seen[tag] = name

		typ := exprString(f.Type)
		k, ok := kinds[typ]
		if !ok {
			return message{}, false, fmt.Errorf("%s.%s: no wire kind for %s", ts.Name.Name, name, typ)
		}
		if opt && k.nonzero == nil {
			return message{}, false, fmt.Errorf(
				"%s.%s: ,opt is for bool/u16/u32 only (empty values are always omitted)", ts.Name.Name, name)
		}
		msg.fields = append(msg.fields, field{name: name, typ: typ, tag: tag, opt: opt, kind: k})
	}
	return msg, true, nil
}

func hasTags(st *ast.StructType) bool {
	for _, f := range st.Fields.List {
		if _, ok := tagLiteral(f); ok {
			return true
		}
	}
	return false
}

func fieldName(msg string, f *ast.Field) (string, string, bool, error) {
	if len(f.Names) != 1 {
		return "", "", false, fmt.Errorf("%s: one field per line, got %d", msg, len(f.Names))
	}
	lit, tagged := tagLiteral(f)
	return f.Names[0].Name, lit, tagged, nil
}

// docOf returns a type declaration's doc comment. The parser hangs it on the
// GenDecl for ungrouped `type X struct` statements and on the TypeSpec inside
// a `type (...)` group, so check both.
func docOf(gd *ast.GenDecl, ts *ast.TypeSpec) *ast.CommentGroup {
	if ts.Doc != nil {
		return ts.Doc
	}
	return gd.Doc
}

// pubName reads an optional `//pxb:pub Name` directive, which keeps the
// Encode/Decode names stable when the struct name cannot supply them.
func pubName(doc *ast.CommentGroup, fallback string) string {
	if doc == nil {
		return fallback
	}
	name := fallback
	for _, c := range doc.List {
		if p, ok := strings.CutPrefix(c.Text, "//pxb:pub "); ok {
			name = strings.TrimSpace(p)
		}
	}
	return name
}

func tagLiteral(f *ast.Field) (string, bool) {
	if f.Tag == nil {
		return "", false
	}
	raw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return "", false
	}
	lit, ok := strings.CutPrefix(raw, `pxb:"`)
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(lit, `"`), true
}

func parseTag(lit string) (uint16, bool, error) {
	value, opts, _ := strings.Cut(lit, ",")
	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, false, fmt.Errorf("bad pxb tag %q", lit)
	}
	switch {
	case n == 0:
		return 0, false, errors.New("tag 0 is reserved")
	case n > maxFieldTag:
		return 0, false, fmt.Errorf("tag %d is past the message range (max %d)", n, maxFieldTag)
	}
	switch opts {
	case "":
		return uint16(n), false, nil
	case "opt":
		return uint16(n), true, nil
	default:
		return 0, false, fmt.Errorf("unknown pxb option %q", opts)
	}
}

func exprString(e ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), e); err != nil {
		return ""
	}
	return buf.String()
}
