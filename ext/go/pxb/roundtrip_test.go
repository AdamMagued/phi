package pxb_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/ext/go/pxb"
)

// roundTrip encodes a message with every field set, decodes it, and requires the
// struct to come back unchanged. Doing it by reflection means one test covers
// every field of every message: a field missing from schema.go, a wrong tag, or
// a decode case the generator got wrong all fail here.
func roundTrip[T any](t *testing.T, encode func(T) []byte, decode func([]byte) (T, error)) {
	t.Helper()

	want := populated[T]()
	raw := encode(want)
	got, err := decode(raw)
	require.NoError(t, err)
	require.Equal(t, want, got, "field lost in encode/decode")
	require.Equal(t, raw, encode(got), "encode is not stable")
}

// TestRoundTripAllMessages covers every type in schema.go, so a new field is
// checked as soon as it is declared. Add a line when you add a message.
func TestRoundTripAllMessages(t *testing.T) {
	roundTrip(t, pxb.EncodeHello, pxb.DecodeHello)
	roundTrip(t, pxb.EncodeHelloAck, pxb.DecodeHelloAck)
	roundTrip(t, pxb.EncodeRegisterCommand, pxb.DecodeRegisterCommand)
	roundTrip(t, pxb.EncodeRegisterTool, pxb.DecodeRegisterTool)
	roundTrip(t, pxb.EncodeToolDetailResult, pxb.DecodeToolDetailResult)
	roundTrip(t, pxb.EncodeSubscribe, pxb.DecodeSubscribe)
	roundTrip(t, pxb.EncodeCommandInvoked, pxb.DecodeCommandInvoked)
	roundTrip(t, pxb.EncodeCommandResponse, pxb.DecodeCommandResponse)
	roundTrip(t, pxb.EncodeToolInvoke, pxb.DecodeToolInvoke)
	roundTrip(t, pxb.EncodeToolResult, pxb.DecodeToolResult)
	roundTrip(t, pxb.EncodeInterceptReq, pxb.DecodeInterceptReq)
	roundTrip(t, pxb.EncodeInterceptResp, pxb.DecodeInterceptResp)
	roundTrip(t, pxb.EncodeEventNotify, pxb.DecodeEventNotify)
	roundTrip(t, pxb.EncodeNotify, pxb.DecodeNotify)
	roundTrip(t, pxb.EncodeHostRequest, pxb.DecodeHostRequest)
	roundTrip(t, pxb.EncodeHostResult, pxb.DecodeHostResult)
	roundTrip(t, pxb.EncodeSessionMeta, pxb.DecodeSessionMeta)
}

// TestEncodeAllocatesOnce pins the sizing contract: encoders reserve the exact
// payload, so one allocation per message and no append growth.
func TestEncodeAllocatesOnce(t *testing.T) {
	cases := []struct {
		name   string
		allocs func(*testing.T) float64
	}{
		{"Hello", allocsPerEncode(pxb.EncodeHello)},
		{"HelloAck", allocsPerEncode(pxb.EncodeHelloAck)},
		{"RegisterCommand", allocsPerEncode(pxb.EncodeRegisterCommand)},
		{"RegisterTool", allocsPerEncode(pxb.EncodeRegisterTool)},
		{"ToolDetailResult", allocsPerEncode(pxb.EncodeToolDetailResult)},
		{"Subscribe", allocsPerEncode(pxb.EncodeSubscribe)},
		{"CommandInvoked", allocsPerEncode(pxb.EncodeCommandInvoked)},
		{"CommandResponse", allocsPerEncode(pxb.EncodeCommandResponse)},
		{"ToolInvoke", allocsPerEncode(pxb.EncodeToolInvoke)},
		{"ToolResult", allocsPerEncode(pxb.EncodeToolResult)},
		{"InterceptReq", allocsPerEncode(pxb.EncodeInterceptReq)},
		{"InterceptResp", allocsPerEncode(pxb.EncodeInterceptResp)},
		{"EventNotify", allocsPerEncode(pxb.EncodeEventNotify)},
		{"Notify", allocsPerEncode(pxb.EncodeNotify)},
		{"HostRequest", allocsPerEncode(pxb.EncodeHostRequest)},
		{"HostResult", allocsPerEncode(pxb.EncodeHostResult)},
		{"SessionMeta", allocsPerEncode(pxb.EncodeSessionMeta)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.LessOrEqual(t, tc.allocs(t), 1.0, "encoder grew its buffer")
		})
	}
}

func allocsPerEncode[T any](encode func(T) []byte) func(*testing.T) float64 {
	return func(t *testing.T) float64 {
		t.Helper()

		msg := populated[T]()
		require.NotEmpty(t, encode(msg), "fixture encodes to nothing")
		return testing.AllocsPerRun(50, func() { _ = encode(msg) })
	}
}

func populated[T any]() T {
	v := reflect.New(reflect.TypeFor[T]()).Elem()
	fill(v)
	return v.Interface().(T)
}

func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Uint8:
		v.SetUint(1)
	case reflect.Uint16:
		v.SetUint(2)
	case reflect.Uint32:
		v.SetUint(3)
	case reflect.Struct:
		for f := range v.Type().Fields() {
			fill(v.FieldByIndex(f.Index))
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0))
		v.Set(s)
	default:
		panic("fill: unhandled kind " + v.Kind().String())
	}
}
