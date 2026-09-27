package pxb

import (
	"encoding/binary"
	"errors"
)

// Tagged payload wire kinds. Only these two exist so unknown fields are
// always skippable without a schema.
const (
	WireU64   uint8 = 1 // 8-byte little-endian (u16/u32/bool/event codes)
	WireBytes uint8 = 2 // u32 length + bytes
)

// Encoded field sizes: a 3-byte field header (tag u16 + kind u8) plus the value,
// or plus a u32 length for blobs. Generated encoders sum these to reserve the
// exact payload, so they must stay in step with the Put* methods below.
const (
	u64FieldSize   = 3 + 8
	blobHeaderSize = 3 + 4
)

var (
	ErrBadWire = errors.New("pxb: bad wire kind")
	ErrBadTag  = errors.New("pxb: bad field tag")
)

// FieldWriter builds a tagged-field payload (protobuf-style, fixed-width).
//
// Layout per field:
//
//	tag u16 | kind u8 | value…
//
// Unknown tags are skipped by kind. New fields must use new tag numbers;
// never reuse or reorder meaning of an existing tag.
type FieldWriter struct {
	b []byte
}

// Reset clears the buffer keeping capacity.
func (fw *FieldWriter) Reset() { fw.b = fw.b[:0] }

// Bytes returns the encoded payload.
func (fw *FieldWriter) Bytes() []byte { return fw.b }

// sizedWriter starts a writer with n bytes reserved. An encoder that knows its
// exact payload size allocates once and never grows.
func sizedWriter(n int) FieldWriter { return FieldWriter{b: make([]byte, 0, n)} }

func (fw *FieldWriter) putHdr(tag uint16, kind uint8) {
	var tmp [3]byte
	binary.LittleEndian.PutUint16(tmp[0:2], tag)
	tmp[2] = kind
	fw.b = append(fw.b, tmp[:]...)
}

// PutU64 writes an 8-byte integer field.
func (fw *FieldWriter) PutU64(tag uint16, v uint64) {
	fw.putHdr(tag, WireU64)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], v)
	fw.b = append(fw.b, tmp[:]...)
}

// PutU16 writes v as WireU64.
func (fw *FieldWriter) PutU16(tag, v uint16) { fw.PutU64(tag, uint64(v)) }

// PutU32 writes v as WireU64.
func (fw *FieldWriter) PutU32(tag uint16, v uint32) { fw.PutU64(tag, uint64(v)) }

// PutBool writes v as WireU64 0/1.
func (fw *FieldWriter) PutBool(tag uint16, v bool) {
	if v {
		fw.PutU64(tag, 1)
	} else {
		fw.PutU64(tag, 0)
	}
}

// PutBytes writes a length-prefixed blob. Empty blobs are omitted so
// decoders stay compact on the hot path.
func (fw *FieldWriter) PutBytes(tag uint16, p []byte) {
	if len(p) == 0 {
		return
	}
	fw.putHdr(tag, WireBytes)
	n := uint32(len(p)) //nolint:gosec // G115: blob length bounded by MaxPayload on the frame path
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], n)
	fw.b = append(fw.b, tmp[:]...)
	fw.b = append(fw.b, p...)
}

// PutString writes s as WireBytes. Empty strings are omitted.
func (fw *FieldWriter) PutString(tag uint16, s string) {
	if s == "" {
		return
	}
	fw.PutBytes(tag, []byte(s))
}

// PutU16s writes a packed list as WireBytes: u16 count + u16 values. It packs in
// place rather than through a ByteWriter so encoding stays single-allocation.
func (fw *FieldWriter) PutU16s(tag uint16, vs []uint16) {
	if len(vs) == 0 {
		return
	}
	fw.putHdr(tag, WireBytes)
	fw.b = binary.LittleEndian.AppendUint32(fw.b, uint32(2+len(vs)*2)) //nolint:gosec // G115: bounded by MaxPayload
	fw.b = binary.LittleEndian.AppendUint16(fw.b, uint16(len(vs)))     //nolint:gosec // G115: bounded by MaxPayload
	for _, v := range vs {
		fw.b = binary.LittleEndian.AppendUint16(fw.b, v)
	}
}

// strSize is the encoded size of a string field, or 0 when it is omitted.
func strSize(s string) int {
	if s == "" {
		return 0
	}
	return blobHeaderSize + len(s)
}

// blobSize is strSize for opaque bytes.
func blobSize(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	return blobHeaderSize + len(p)
}

// u16sSize is the encoded size of a packed u16 list (count + values).
func u16sSize(vs []uint16) int {
	if len(vs) == 0 {
		return 0
	}
	return blobHeaderSize + 2 + len(vs)*2
}

// FieldReader walks a tagged-field payload.
type FieldReader struct {
	b []byte
	i int
}

// NewFieldReader wraps b.
func NewFieldReader(b []byte) *FieldReader { return &FieldReader{b: b} }

// Done reports whether all bytes were consumed.
func (fr *FieldReader) Done() bool { return fr.i >= len(fr.b) }

func (fr *FieldReader) need(n int) error {
	if len(fr.b)-fr.i < n {
		return ErrTruncated
	}
	return nil
}

// Next returns the next field tag and wire kind.
func (fr *FieldReader) Next() (tag uint16, kind uint8, err error) {
	if fr.Done() {
		return 0, 0, ErrTruncated
	}
	if err := fr.need(3); err != nil {
		return 0, 0, err
	}
	tag = uint16(fr.b[fr.i]) | uint16(fr.b[fr.i+1])<<8
	kind = fr.b[fr.i+2]
	fr.i += 3
	if kind != WireU64 && kind != WireBytes {
		return tag, kind, ErrBadWire
	}
	if tag == 0 {
		return tag, kind, ErrBadTag
	}
	return tag, kind, nil
}

// U64 reads a WireU64 value (call after Next returned WireU64).
func (fr *FieldReader) U64() (uint64, error) {
	if err := fr.need(8); err != nil {
		return 0, err
	}
	b := fr.b[fr.i:]
	fr.i += 8
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56, nil
}

// Bytes reads a WireBytes value (call after Next returned WireBytes).
// The returned slice aliases the underlying buffer.
func (fr *FieldReader) Bytes() ([]byte, error) {
	if err := fr.need(4); err != nil {
		return nil, err
	}
	n := uint32(fr.b[fr.i]) | uint32(fr.b[fr.i+1])<<8 | uint32(fr.b[fr.i+2])<<16 | uint32(fr.b[fr.i+3])<<24
	fr.i += 4
	if err := fr.need(int(n)); err != nil {
		return nil, err
	}
	out := fr.b[fr.i : fr.i+int(n)]
	fr.i += int(n)
	return out, nil
}

// Skip discards the value for kind.
func (fr *FieldReader) Skip(kind uint8) error {
	switch kind {
	case WireU64:
		_, err := fr.U64()
		return err
	case WireBytes:
		_, err := fr.Bytes()
		return err
	default:
		return ErrBadWire
	}
}

// Walk calls fn for each field. Unknown tags should Skip via the reader.
// fn may call U64/Bytes exactly once for the current field, or Skip.
func Walk(b []byte, fn func(tag uint16, kind uint8, fr *FieldReader) error) error {
	fr := NewFieldReader(b)
	for !fr.Done() {
		tag, kind, err := fr.Next()
		if err != nil {
			return err
		}
		before := fr.i
		if err := fn(tag, kind, fr); err != nil {
			return err
		}
		// If the callback neither consumed nor skipped, skip for them.
		if fr.i == before {
			if err := fr.Skip(kind); err != nil {
				return err
			}
		}
	}
	return nil
}

// takeU64 reads a WireU64 value, rejecting a field that claims another kind.
func takeU64(kind uint8, fr *FieldReader) (uint64, error) {
	if kind != WireU64 {
		_ = fr.Skip(kind)
		return 0, ErrBadWire
	}
	return fr.U64()
}

// takeBytes reads a WireBytes value, rejecting a field that claims another kind.
func takeBytes(kind uint8, fr *FieldReader) ([]byte, error) {
	if kind != WireBytes {
		_ = fr.Skip(kind)
		return nil, ErrBadWire
	}
	return fr.Bytes()
}

// takeString reads a WireBytes value as a string.
func takeString(kind uint8, fr *FieldReader) (string, error) {
	p, err := takeBytes(kind, fr)
	if err != nil {
		return "", err
	}
	return string(p), nil
}

// decodeU16s reads a packed u16 list written by PutU16s.
func decodeU16s(p []byte) ([]uint16, error) {
	return NewByteReader(p).U16s()
}
