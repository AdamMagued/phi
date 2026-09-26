// Command pxbgen writes msg_gen.go from the `pxb` struct tags in schema.go.
//
// It reads and writes the package directory it runs in, so all of these work:
//
//	make generate              # repo root
//	go generate ./pxb           # from ext/go
//	go run ../internal/pxbgen   # from ext/go/pxb
//
// The generator is strict on purpose. A missing, duplicated, out-of-range, or
// unmapped tag fails the run instead of emitting a payload that cannot interop
// with the other SDK. `-check` reports a stale msg_gen.go without writing it.
//
// Generated encoders reserve the exact payload size, which is why the size
// helpers exist: one allocation per message, no append growth, no pooling.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strconv"
	"strings"
)

const (
	schemaFile = "schema.go"
	outputFile = "msg_gen.go"

	// maxFieldTag is the last tag an application message may use: 64-127 are
	// reserved for cross-cutting fields and 128+ for experiments, and both
	// ranges are protocol decisions rather than schema edits.
	maxFieldTag = 63
)

func main() {
	check := flag.Bool("check", false, "fail if "+outputFile+" is stale instead of rewriting it")
	flag.Parse()
	if err := run(*check); err != nil {
		log.Fatal(err)
	}
}

func run(check bool) error {
	msgs, err := parseSchema(schemaFile)
	if err != nil {
		return err
	}
	src, err := generate(msgs)
	if err != nil {
		return err
	}
	if !check {
		// Generated source is world-readable, like every other file in the tree.
		return os.WriteFile(outputFile, src, 0o644) //nolint:gosec // G306: not a secret
	}
	old, err := os.ReadFile(outputFile)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, src) {
		return fmt.Errorf("%s is stale: run make generate", outputFile)
	}
	return nil
}

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
		name, tagged, err := fieldName(ts.Name.Name, f)
		if err != nil {
			return message{}, false, err
		}
		if !tagged {
			return message{}, false, fmt.Errorf("%s.%s: message fields need a pxb tag", ts.Name.Name, name)
		}
		lit, _ := tagLiteral(f)
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

func fieldName(msg string, f *ast.Field) (string, bool, error) {
	if len(f.Names) != 1 {
		return "", false, fmt.Errorf("%s: one field per line, got %d", msg, len(f.Names))
	}
	_, tagged := tagLiteral(f)
	return f.Names[0].Name, tagged, nil
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

func generate(msgs []message) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by ext/go/internal/pxbgen from schema.go. DO NOT EDIT.\n\n")
	buf.WriteString("package pxb\n")
	for _, m := range msgs {
		writeMessage(&buf, m)
	}
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return src, nil
}

func writeMessage(w *bytes.Buffer, m message) {
	fmt.Fprintf(w, "\n// %s field tags.\nconst (\n", m.name)
	for _, f := range m.fields {
		fmt.Fprintf(w, "\tf%s%s uint16 = %d\n", m.pub, f.name, f.tag)
	}
	w.WriteString(")\n")
	writeSize(w, m)
	writeEncode(w, m)
	writeDecode(w, m)
}

// writeSize emits the exact encoded size, so writeEncode can reserve it once.
func writeSize(w *bytes.Buffer, m message) {
	fmt.Fprintf(w, "\n// size%s is the exact wire size of msg.\nfunc size%s(msg %s) int {\n\tn := 0\n",
		m.pub, m.pub, m.name)
	for _, f := range m.fields {
		for _, line := range f.kind.size("msg."+f.name, f.opt) {
			fmt.Fprintf(w, "\t%s\n", line)
		}
	}
	w.WriteString("\treturn n\n}\n")
}

func writeEncode(w *bytes.Buffer, m message) {
	fmt.Fprintf(w, "\nfunc Encode%s(msg %s) []byte {\n", m.pub, m.name)
	fmt.Fprintf(w, "\tn := size%s(msg)\n\tif n == 0 {\n\t\treturn nil\n\t}\n\tfw := sizedWriter(n)\n", m.pub)
	for _, f := range m.fields {
		call := f.kind.put(tagConst(m, f), "msg."+f.name)
		if !f.opt {
			fmt.Fprintf(w, "\t%s\n", call)
			continue
		}
		fmt.Fprintf(w, "\tif %s {\n\t\t%s\n\t}\n", f.kind.nonzero("msg."+f.name), call)
	}
	w.WriteString("\treturn fw.Bytes()\n}\n")
}

func writeDecode(w *bytes.Buffer, m message) {
	fmt.Fprintf(w, "\nfunc Decode%s(b []byte) (%s, error) {\n\tvar msg %s\n", m.pub, m.name, m.name)
	w.WriteString("\terr := Walk(b, func(tag uint16, kind uint8, fr *FieldReader) error {\n\t\tswitch tag {\n")
	for _, f := range m.fields {
		fmt.Fprintf(w, "\t\tcase %s:\n", tagConst(m, f))
		for _, line := range f.kind.take("msg." + f.name) {
			fmt.Fprintf(w, "\t\t\t%s\n", line)
		}
	}
	w.WriteString("\t\tdefault:\n\t\t\treturn fr.Skip(kind)\n\t\t}\n\t})\n\treturn msg, err\n}\n")
}

func tagConst(m message, f field) string { return "f" + m.pub + f.name }

// kind maps a Go field type onto the wire: how to write it, how to read it,
// how many bytes it takes, and what `,opt` means for it.
type kind struct {
	put     func(tagConst, expr string) string
	take    func(target string) []string
	size    func(expr string, opt bool) []string
	nonzero func(expr string) string
}

// u64Kind builds the kind for the fixed-width WireU64 fields. conv is the
// narrowing from the decoded u64 ("" for bool, which needs no comment) and
// nonzero decides whether an optional field is worth writing.
func u64Kind(method, conv, wire string, nonzero func(string) string) kind {
	assign := func(target string) string {
		if conv == "" {
			return target + " = v != 0"
		}
		return fmt.Sprintf("%s = %s(v) //nolint:gosec // G115: %s by protocol", target, conv, wire)
	}
	return kind{
		put: func(constName, expr string) string { return fmt.Sprintf("fw.%s(%s, %s)", method, constName, expr) },
		take: func(target string) []string {
			return []string{"v, err := takeU64(kind, fr)", assign(target), "return err"}
		},
		size: func(expr string, opt bool) []string {
			if !opt {
				return []string{"n += u64FieldSize"}
			}
			return []string{"if " + nonzero(expr) + " {", "n += u64FieldSize", "}"}
		},
		nonzero: nonzero,
	}
}

var kinds = map[string]kind{
	"string": {
		put: func(constName, expr string) string { return fmt.Sprintf("fw.PutString(%s, %s)", constName, expr) },
		take: func(target string) []string {
			return []string{"s, err := takeString(kind, fr)", target + " = s", "return err"}
		},
		size: func(expr string, _ bool) []string { return []string{"n += strSize(" + expr + ")"} },
	},
	"[]byte": {
		put: func(constName, expr string) string { return fmt.Sprintf("fw.PutBytes(%s, %s)", constName, expr) },
		take: func(target string) []string {
			return []string{"p, err := takeBytes(kind, fr)", target + " = append([]byte(nil), p...)", "return err"}
		},
		size: func(expr string, _ bool) []string { return []string{"n += blobSize(" + expr + ")"} },
	},
	"[]uint16": {
		put: func(constName, expr string) string { return fmt.Sprintf("fw.PutU16s(%s, %s)", constName, expr) },
		take: func(target string) []string {
			return []string{
				"p, err := takeBytes(kind, fr)",
				"if err != nil {",
				"\treturn err",
				"}",
				target + ", err = decodeU16s(p)",
				"return err",
			}
		},
		size: func(expr string, _ bool) []string { return []string{"n += u16sSize(" + expr + ")"} },
	},
	"bool":   u64Kind("PutBool", "", "", func(expr string) string { return expr }),
	"uint16": u64Kind("PutU16", "uint16", "u16", func(expr string) string { return expr + " > 0" }),
	"uint32": u64Kind("PutU32", "uint32", "u32", func(expr string) string { return expr + " > 0" }),
}
