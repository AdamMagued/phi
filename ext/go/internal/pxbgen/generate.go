package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"text/template"
)

// fieldTemplateData holds pre-rendered strings for a single field, so the
// template only deals with plain data and no function calls.
type fieldTemplateData struct {
	Name      string
	Tag       uint16
	Opt       bool
	TagConst  string
	PutCall   string
	Nonzero   string
	SizeLines []string
	TakeLines []string
}

// messageTemplateData wraps a message with pre-rendered field data for
// template execution.
type messageTemplateData struct {
	message
	Fields []fieldTemplateData
	Name   string
	Pub    string
}

// templateData wraps pre-rendered messages for template execution.
type templateData struct {
	Messages []messageTemplateData
}

func buildTemplateData(msgs []message) []messageTemplateData {
	result := make([]messageTemplateData, 0, len(msgs))
	for _, m := range msgs {
		fd := make([]fieldTemplateData, 0, len(m.fields))
		for _, f := range m.fields {
			tc := tagConst(m, f)
			var nz string
			if f.opt {
				nz = f.kind.nonzero("msg." + f.name)
			}
			fd = append(fd, fieldTemplateData{
				Name:      f.name,
				Tag:       f.tag,
				Opt:       f.opt,
				TagConst:  tc,
				PutCall:   f.kind.put(tc, "msg."+f.name),
				Nonzero:   nz,
				SizeLines: f.kind.size("msg."+f.name, f.opt),
				TakeLines: f.kind.take("msg." + f.name),
			})
		}
		result = append(result, messageTemplateData{message: m, Fields: fd, Name: m.name, Pub: m.pub})
	}
	return result
}

//go:embed template/*
var templateFS embed.FS

var msgGenTemplate = template.Must(template.New("msg_gen.go").ParseFS(templateFS, "template/msg_gen.go.tmpl"))

func generate(msgs []message) ([]byte, error) {
	var buf bytes.Buffer
	data := templateData{Messages: buildTemplateData(msgs)}
	if err := msgGenTemplate.ExecuteTemplate(&buf, "msg_gen.go.tmpl", data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return src, nil
}

func tagConst(m message, f field) string { return "f" + m.pub + f.name }
