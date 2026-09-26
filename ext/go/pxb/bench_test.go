package pxb_test

import (
	"testing"

	"github.com/pulseaiclub/phi/ext/go/pxb"
)

var (
	benchIntercept = pxb.InterceptReq{
		Event: pxb.EvToolCall, ToolName: "bash", ToolCallID: "call_1",
		Input:     []byte(`{"command":"go test ./...","timeout":120}`),
		TurnIndex: 7,
	}
	benchToolResult = pxb.ToolResultMsg{
		Content: "ok\n", Detail: "internal/session", Output: "PASS\n", Expanded: true,
	}
	benchSubscribe = pxb.Subscribe{
		Events:    []uint16{pxb.EvSessionStart, pxb.EvToolCall, pxb.EvTurnEnd},
		Intercept: []uint16{pxb.EvToolCall},
	}

	sinkBytes []byte
	sinkIx    pxb.InterceptReq
	sinkTool  pxb.ToolResultMsg
	sinkSub   pxb.Subscribe
)

func BenchmarkEncodeInterceptReq(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkBytes = pxb.EncodeInterceptReq(benchIntercept)
	}
}

func BenchmarkDecodeInterceptReq(b *testing.B) {
	raw := pxb.EncodeInterceptReq(benchIntercept)
	b.ReportAllocs()
	for b.Loop() {
		v, err := pxb.DecodeInterceptReq(raw)
		if err != nil {
			b.Fatal(err)
		}
		sinkIx = v
	}
}

func BenchmarkEncodeToolResult(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkBytes = pxb.EncodeToolResult(benchToolResult)
	}
}

func BenchmarkDecodeToolResult(b *testing.B) {
	raw := pxb.EncodeToolResult(benchToolResult)
	b.ReportAllocs()
	for b.Loop() {
		v, err := pxb.DecodeToolResult(raw)
		if err != nil {
			b.Fatal(err)
		}
		sinkTool = v
	}
}

func BenchmarkEncodeSubscribe(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkBytes = pxb.EncodeSubscribe(benchSubscribe)
	}
}

func BenchmarkDecodeSubscribe(b *testing.B) {
	raw := pxb.EncodeSubscribe(benchSubscribe)
	b.ReportAllocs()
	for b.Loop() {
		v, err := pxb.DecodeSubscribe(raw)
		if err != nil {
			b.Fatal(err)
		}
		sinkSub = v
	}
}
