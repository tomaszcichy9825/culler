package preview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

// --- fixture helpers -------------------------------------------------------

// jpegBlob builds a fake JPEG payload of n bytes: SOI, a run of fill bytes
// unique to this blob, EOI. n must be at least 4.
func jpegBlob(fill byte, n int) []byte {
	if n < 4 {
		panic("jpeg blob too small")
	}
	b := make([]byte, n)
	b[0], b[1] = 0xFF, 0xD8
	for i := 2; i < n-2; i++ {
		b[i] = fill
	}
	b[n-2], b[n-1] = 0xFF, 0xD9
	return b
}

type ifdEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	value uint32
}

func longEntry(tag uint16, v uint32) ifdEntry {
	return ifdEntry{tag: tag, typ: 4, count: 1, value: v}
}

// shortEntry packs a single SHORT into the value field, which sits in the
// first two bytes of that field regardless of byte order.
func shortEntry(order binary.ByteOrder, tag uint16, v uint16) ifdEntry {
	raw := make([]byte, 4)
	order.PutUint16(raw, v)
	return ifdEntry{tag: tag, typ: 3, count: 1, value: order.Uint32(raw)}
}

// tiffBuilder assembles a minimal TIFF container. Blobs and IFDs are appended
// in call order and their offsets returned, so pointers can be patched once
// every part is placed.
type tiffBuilder struct {
	order binary.ByteOrder
	buf   []byte
}

func newTIFF(order binary.ByteOrder) *tiffBuilder {
	b := &tiffBuilder{order: order, buf: make([]byte, 8)}
	if order == binary.BigEndian {
		b.buf[0], b.buf[1] = 'M', 'M'
	} else {
		b.buf[0], b.buf[1] = 'I', 'I'
	}
	order.PutUint16(b.buf[2:], 42)
	return b
}

func (b *tiffBuilder) blob(p []byte) uint32 {
	off := uint32(len(b.buf))
	b.buf = append(b.buf, p...)
	return off
}

func (b *tiffBuilder) ifd(entries ...ifdEntry) uint32 {
	off := uint32(len(b.buf))
	hdr := make([]byte, 2)
	b.order.PutUint16(hdr, uint16(len(entries)))
	b.buf = append(b.buf, hdr...)
	for _, e := range entries {
		raw := make([]byte, 12)
		b.order.PutUint16(raw[0:], e.tag)
		b.order.PutUint16(raw[2:], e.typ)
		b.order.PutUint32(raw[4:], e.count)
		b.order.PutUint32(raw[8:], e.value)
		b.buf = append(b.buf, raw...)
	}
	b.buf = append(b.buf, 0, 0, 0, 0) // next-IFD pointer
	return off
}

func (b *tiffBuilder) setNext(ifdOff, next uint32) {
	n := uint32(b.order.Uint16(b.buf[ifdOff:]))
	b.order.PutUint32(b.buf[ifdOff+2+12*n:], next)
}

func (b *tiffBuilder) setIFD0(off uint32) { b.order.PutUint32(b.buf[4:], off) }

func (b *tiffBuilder) setMagic(v uint16) { b.order.PutUint16(b.buf[2:], v) }

func (b *tiffBuilder) bytes() []byte { return b.buf }

// simpleTIFF is a container whose IFD0 points at one preview via the
// JPEGInterchangeFormat pair.
func simpleTIFF(order binary.ByteOrder, payload []byte) []byte {
	b := newTIFF(order)
	off := b.blob(payload)
	ifd0 := b.ifd(
		longEntry(tagJPEGOffset, off),
		longEntry(tagJPEGLength, uint32(len(payload))),
	)
	b.setIFD0(ifd0)
	return b.bytes()
}

// rafFile builds a Fujifilm RAF container with the preview at the given
// offset and length written into the fixed header slots.
func rafFile(payload []byte) []byte {
	buf := make([]byte, 128)
	copy(buf, "FUJIFILMCCD-RAW ")
	off := uint32(len(buf))
	binary.BigEndian.PutUint32(buf[84:], off)
	binary.BigEndian.PutUint32(buf[88:], uint32(len(payload)))
	return append(buf, payload...)
}

// jpegWithEXIFThumb builds a JPEG carrying an APP0 segment then an APP1 EXIF
// segment whose IFD1 holds the thumbnail.
func jpegWithEXIFThumb(order binary.ByteOrder, thumb []byte) []byte {
	return jpegWithEXIFThumbAt(order, thumb, nil, nil)
}

// jpegWithEXIFThumbAt is jpegWithEXIFThumb with the IFD1 offset and length
// entries overridable, so tests can point them somewhere hostile.
func jpegWithEXIFThumbAt(order binary.ByteOrder, thumb []byte, offVal, lenVal *uint32) []byte {
	b := newTIFF(order)
	ifd0 := b.ifd(shortEntry(order, 0x0112, 1)) // Orientation, ignored
	off := b.blob(thumb)
	length := uint32(len(thumb))
	if offVal != nil {
		off = *offVal
	}
	if lenVal != nil {
		length = *lenVal
	}
	ifd1 := b.ifd(
		longEntry(tagJPEGOffset, off),
		longEntry(tagJPEGLength, length),
	)
	b.setNext(ifd0, ifd1)
	b.setIFD0(ifd0)

	payload := append([]byte("Exif\x00\x00"), b.bytes()...)
	out := []byte{0xFF, 0xD8}
	out = append(out, app0Segment()...)
	out = append(out, segment(0xE1, payload)...)
	out = append(out, 0xFF, 0xD9)
	return out
}

func app0Segment() []byte {
	return segment(0xE0, []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00"))
}

// segment wraps a payload in a JPEG marker segment with its two-byte length.
func segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

func orders() []struct {
	name  string
	order binary.ByteOrder
} {
	return []struct {
		name  string
		order binary.ByteOrder
	}{
		{"little-endian", binary.LittleEndian},
		{"big-endian", binary.BigEndian},
	}
}

// --- TIFF/IFD extraction ---------------------------------------------------

func TestExtractLargestJPEGFromTIFF(t *testing.T) {
	for _, o := range orders() {
		t.Run(o.name, func(t *testing.T) {
			order := o.order

			t.Run("jpeg interchange pair in IFD0", func(t *testing.T) {
				want := jpegBlob(0x11, 64)
				got, err := ExtractLargestJPEG(simpleTIFF(order, want))
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("payload mismatch: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("strip offsets pair", func(t *testing.T) {
				want := jpegBlob(0x22, 96)
				b := newTIFF(order)
				off := b.blob(want)
				ifd0 := b.ifd(
					longEntry(tagStripOffsets, off),
					longEntry(tagStripByteCounts, uint32(len(want))),
				)
				b.setIFD0(ifd0)

				got, err := ExtractLargestJPEG(b.bytes())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("payload mismatch: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("short typed offsets", func(t *testing.T) {
				want := jpegBlob(0x33, 40)
				b := newTIFF(order)
				off := b.blob(want)
				if off > math.MaxUint16 {
					t.Fatalf("offset %d does not fit a SHORT", off)
				}
				ifd0 := b.ifd(
					shortEntry(order, tagStripOffsets, uint16(off)),
					shortEntry(order, tagStripByteCounts, uint16(len(want))),
				)
				b.setIFD0(ifd0)

				got, err := ExtractLargestJPEG(b.bytes())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("payload mismatch: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("largest of several candidates wins", func(t *testing.T) {
				small := jpegBlob(0x44, 32)
				want := jpegBlob(0x55, 256)
				medium := jpegBlob(0x66, 128)

				b := newTIFF(order)
				smallOff := b.blob(small)
				wantOff := b.blob(want)
				medOff := b.blob(medium)
				ifd1 := b.ifd(
					longEntry(tagJPEGOffset, wantOff),
					longEntry(tagJPEGLength, uint32(len(want))),
				)
				ifd0 := b.ifd(
					longEntry(tagStripOffsets, smallOff),
					longEntry(tagStripByteCounts, uint32(len(small))),
					longEntry(tagJPEGOffset, medOff),
					longEntry(tagJPEGLength, uint32(len(medium))),
				)
				b.setNext(ifd0, ifd1)
				b.setIFD0(ifd0)

				got, err := ExtractLargestJPEG(b.bytes())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("wrong candidate: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("candidate in SubIFD", func(t *testing.T) {
				small := jpegBlob(0x77, 32)
				want := jpegBlob(0x88, 512)

				b := newTIFF(order)
				smallOff := b.blob(small)
				wantOff := b.blob(want)
				sub := b.ifd(
					longEntry(tagStripOffsets, wantOff),
					longEntry(tagStripByteCounts, uint32(len(want))),
				)
				ifd0 := b.ifd(
					longEntry(tagJPEGOffset, smallOff),
					longEntry(tagJPEGLength, uint32(len(small))),
					longEntry(tagSubIFDs, sub),
				)
				b.setIFD0(ifd0)

				got, err := ExtractLargestJPEG(b.bytes())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("SubIFD candidate not found: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("candidate in EXIF IFD", func(t *testing.T) {
				want := jpegBlob(0x99, 300)
				b := newTIFF(order)
				off := b.blob(want)
				exif := b.ifd(
					longEntry(tagJPEGOffset, off),
					longEntry(tagJPEGLength, uint32(len(want))),
				)
				ifd0 := b.ifd(longEntry(tagEXIFIFD, exif))
				b.setIFD0(ifd0)

				got, err := ExtractLargestJPEG(b.bytes())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("EXIF IFD candidate not found: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("larger non-JPEG candidate is skipped", func(t *testing.T) {
				want := jpegBlob(0xAA, 64)
				raw := make([]byte, 4096) // raw sensor strip, no SOI

				b := newTIFF(order)
				rawOff := b.blob(raw)
				wantOff := b.blob(want)
				ifd0 := b.ifd(
					longEntry(tagStripOffsets, rawOff),
					longEntry(tagStripByteCounts, uint32(len(raw))),
					longEntry(tagJPEGOffset, wantOff),
					longEntry(tagJPEGLength, uint32(len(want))),
				)
				b.setIFD0(ifd0)

				got, err := ExtractLargestJPEG(b.bytes())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("non-JPEG strip was not skipped: got %d bytes, want %d", len(got), len(want))
				}
			})

			t.Run("returned bytes do not alias the input", func(t *testing.T) {
				want := jpegBlob(0xBB, 64)
				data := simpleTIFF(order, want)
				got, err := ExtractLargestJPEG(data)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for i := range data {
					data[i] = 0
				}
				if !bytes.Equal(got, want) {
					t.Error("result aliases the input buffer")
				}
			})
		})
	}
}

func TestExtractLargestJPEGAlternativeMagic(t *testing.T) {
	// RW2 uses 0x55 and ORF uses 'OR'/'SR' where baseline TIFF has 42.
	for _, magic := range []uint16{42, 0x55, 0x4F52, 0x5352} {
		want := jpegBlob(0xCC, 48)
		b := newTIFF(binary.LittleEndian)
		off := b.blob(want)
		ifd0 := b.ifd(
			longEntry(tagJPEGOffset, off),
			longEntry(tagJPEGLength, uint32(len(want))),
		)
		b.setIFD0(ifd0)
		b.setMagic(magic)

		got, err := ExtractLargestJPEG(b.bytes())
		if err != nil {
			t.Fatalf("magic %#04x: unexpected error: %v", magic, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("magic %#04x: payload mismatch", magic)
		}
	}
}

// --- RAF -------------------------------------------------------------------

func TestExtractLargestJPEGFromRAF(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		want := jpegBlob(0xDD, 200)
		got, err := ExtractLargestJPEG(rafFile(want))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("payload mismatch: got %d bytes, want %d", len(got), len(want))
		}
	})

	t.Run("errors", func(t *testing.T) {
		valid := rafFile(jpegBlob(0xEE, 200))

		tests := []struct {
			name   string
			mutate func([]byte) []byte
		}{
			{"offset past end", func(d []byte) []byte {
				binary.BigEndian.PutUint32(d[84:], uint32(len(d))+1)
				return d
			}},
			{"length past end", func(d []byte) []byte {
				binary.BigEndian.PutUint32(d[88:], uint32(len(d)))
				return d
			}},
			{"offset plus length overflows", func(d []byte) []byte {
				binary.BigEndian.PutUint32(d[84:], math.MaxUint32-8)
				binary.BigEndian.PutUint32(d[88:], math.MaxUint32-8)
				return d
			}},
			{"zero length", func(d []byte) []byte {
				binary.BigEndian.PutUint32(d[88:], 0)
				return d
			}},
			{"payload lacks SOI", func(d []byte) []byte {
				d[128] = 0x00
				return d
			}},
			{"header only", func(d []byte) []byte { return d[:96] }},
			{"truncated header", func(d []byte) []byte { return d[:80] }},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				data := tc.mutate(bytes.Clone(valid))
				if got, err := ExtractLargestJPEG(data); err == nil {
					t.Fatalf("want error, got %d bytes", len(got))
				}
			})
		}
	})
}

// --- error and hostile input cases -----------------------------------------

func TestExtractLargestJPEGErrors(t *testing.T) {
	order := binary.LittleEndian
	valid := simpleTIFF(order, jpegBlob(0x01, 64))

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"nil input", nil, ErrUnsupported},
		{"empty input", []byte{}, ErrUnsupported},
		{"single byte", []byte{'I'}, ErrUnsupported},
		{"unknown magic", []byte("NOTARAWFILEATALL0123456789"), ErrUnsupported},
		{"jpeg is not a container", jpegBlob(0x02, 64), ErrUnsupported},
		{"bad TIFF version", func() []byte {
			d := bytes.Clone(valid)
			order.PutUint16(d[2:], 43) // BigTIFF, not supported
			return d
		}(), ErrUnsupported},
		{"header truncated", valid[:6], ErrUnsupported},
		{"IFD0 offset past end", func() []byte {
			d := bytes.Clone(valid)
			order.PutUint32(d[4:], uint32(len(d))+16)
			return d
		}(), ErrNoPreview},
		{"IFD0 offset overflows", func() []byte {
			d := bytes.Clone(valid)
			order.PutUint32(d[4:], math.MaxUint32)
			return d
		}(), ErrNoPreview},
		{"IFD0 offset inside header", func() []byte {
			d := bytes.Clone(valid)
			order.PutUint32(d[4:], 2)
			return d
		}(), ErrNoPreview},
		{"truncated one byte into the IFD", valid[:len(valid)-29], ErrNoPreview},
		{"truncated mid entry", valid[:len(valid)-20], ErrNoPreview},
		{"truncated before the IFD", valid[:len(valid)-40], ErrNoPreview},
		{"absurd entry count", func() []byte {
			d := bytes.Clone(valid)
			ifd0 := order.Uint32(d[4:])
			order.PutUint16(d[ifd0:], math.MaxUint16)
			return d
		}(), ErrNoPreview},
		{"preview offset past end", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(
				longEntry(tagJPEGOffset, 1<<20),
				longEntry(tagJPEGLength, 64),
			)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"preview length past end", func() []byte {
			b := newTIFF(order)
			off := b.blob(jpegBlob(0x03, 64))
			ifd0 := b.ifd(
				longEntry(tagJPEGOffset, off),
				longEntry(tagJPEGLength, 1<<20),
			)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"offset plus length overflows uint32", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(
				longEntry(tagJPEGOffset, math.MaxUint32-16),
				longEntry(tagJPEGLength, math.MaxUint32-16),
			)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"length without offset", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(longEntry(tagJPEGLength, 64))
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"value array offset past end", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(
				ifdEntry{tag: tagStripOffsets, typ: 4, count: 64, value: 1 << 20},
				ifdEntry{tag: tagStripByteCounts, typ: 4, count: 64, value: 1 << 20},
			)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"value count overflows", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(
				ifdEntry{tag: tagStripOffsets, typ: 4, count: math.MaxUint32, value: 8},
				ifdEntry{tag: tagStripByteCounts, typ: 4, count: math.MaxUint32, value: 8},
			)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"payload is not a JPEG", func() []byte {
			b := newTIFF(order)
			off := b.blob(make([]byte, 64))
			ifd0 := b.ifd(
				longEntry(tagJPEGOffset, off),
				longEntry(tagJPEGLength, 64),
			)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"IFD chain loops on itself", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(longEntry(0x010E, 0))
			b.setNext(ifd0, ifd0)
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
		{"SubIFD points at its parent", func() []byte {
			b := newTIFF(order)
			ifd0 := b.ifd(longEntry(tagSubIFDs, 8))
			b.setIFD0(ifd0)
			return b.bytes()
		}(), ErrNoPreview},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractLargestJPEG(tc.data)
			if err == nil {
				t.Fatalf("want error, got %d bytes", len(got))
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("want %v, got %v", tc.want, err)
			}
			if got != nil {
				t.Errorf("want nil payload on error, got %d bytes", len(got))
			}
		})
	}
}

// TestExtractLargestJPEGDeepIFDChain checks the walk terminates on a chain
// longer than the cap rather than running away.
func TestExtractLargestJPEGDeepIFDChain(t *testing.T) {
	order := binary.LittleEndian
	want := jpegBlob(0x04, 64)

	b := newTIFF(order)
	off := b.blob(want)
	offsets := make([]uint32, 0, 64)
	for i := 0; i < 64; i++ {
		offsets = append(offsets, b.ifd(longEntry(0x010E, 0)))
	}
	// Only the last IFD in the chain holds the preview, beyond the cap.
	last := b.ifd(
		longEntry(tagJPEGOffset, off),
		longEntry(tagJPEGLength, uint32(len(want))),
	)
	offsets = append(offsets, last)
	for i := 0; i < len(offsets)-1; i++ {
		b.setNext(offsets[i], offsets[i+1])
	}
	b.setIFD0(offsets[0])

	if _, err := ExtractLargestJPEG(b.bytes()); !errors.Is(err, ErrNoPreview) {
		t.Fatalf("want ErrNoPreview once the IFD cap is hit, got %v", err)
	}
}

// --- EXIF thumbnail --------------------------------------------------------

func TestExtractEXIFThumb(t *testing.T) {
	for _, o := range orders() {
		t.Run(o.name, func(t *testing.T) {
			want := jpegBlob(0x05, 160)
			got, err := ExtractEXIFThumb(jpegWithEXIFThumb(o.order, want))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("thumbnail mismatch: got %d bytes, want %d", len(got), len(want))
			}
		})
	}

	t.Run("returned bytes do not alias the input", func(t *testing.T) {
		want := jpegBlob(0x06, 64)
		data := jpegWithEXIFThumb(binary.LittleEndian, want)
		got, err := ExtractEXIFThumb(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i := range data {
			data[i] = 0
		}
		if !bytes.Equal(got, want) {
			t.Error("result aliases the input buffer")
		}
	})
}

func TestExtractEXIFThumbErrors(t *testing.T) {
	order := binary.LittleEndian
	thumb := jpegBlob(0x07, 64)

	noThumb := func() []byte {
		b := newTIFF(order)
		ifd0 := b.ifd(shortEntry(order, 0x0112, 1))
		b.setIFD0(ifd0)
		payload := append([]byte("Exif\x00\x00"), b.bytes()...)
		out := []byte{0xFF, 0xD8}
		out = append(out, segment(0xE1, payload)...)
		return append(out, 0xFF, 0xD9)
	}

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"nil input", nil, ErrUnsupported},
		{"empty input", []byte{}, ErrUnsupported},
		{"not a JPEG", []byte("just some bytes here"), ErrUnsupported},
		{"SOI only", []byte{0xFF, 0xD8}, ErrNoThumbnail},
		{"no APP1 segment", func() []byte {
			out := []byte{0xFF, 0xD8}
			out = append(out, app0Segment()...)
			return append(out, 0xFF, 0xD9)
		}(), ErrNoThumbnail},
		{"APP1 without EXIF header", func() []byte {
			out := []byte{0xFF, 0xD8}
			out = append(out, segment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00"))...)
			return append(out, 0xFF, 0xD9)
		}(), ErrNoThumbnail},
		{"segment length below minimum", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x01, 0x00}, ErrNoThumbnail},
		{"segment length past end", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF, 0x00}, ErrNoThumbnail},
		{"no IFD1 in the chain", noThumb(), ErrNoThumbnail},
		{"thumbnail offset past segment", func() []byte {
			bad := uint32(1 << 20)
			return jpegWithEXIFThumbAt(order, thumb, &bad, nil)
		}(), ErrNoThumbnail},
		{"thumbnail length past segment", func() []byte {
			bad := uint32(1 << 20)
			return jpegWithEXIFThumbAt(order, thumb, nil, &bad)
		}(), ErrNoThumbnail},
		{"thumbnail offset plus length overflows", func() []byte {
			bad := uint32(math.MaxUint32 - 16)
			return jpegWithEXIFThumbAt(order, thumb, &bad, &bad)
		}(), ErrNoThumbnail},
		{"scan starts before any EXIF", func() []byte {
			out := []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x02}
			return out
		}(), ErrNoThumbnail},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractEXIFThumb(tc.data)
			if err == nil {
				t.Fatalf("want error, got %d bytes", len(got))
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("want %v, got %v", tc.want, err)
			}
			if got != nil {
				t.Errorf("want nil payload on error, got %d bytes", len(got))
			}
		})
	}
}

// --- truncation and corruption ---------------------------------------------

// fixtures returns one of every container this package understands, for the
// truncation sweep and the fuzz corpus.
func fixtures() map[string][]byte {
	return map[string][]byte{
		"tiff-le": simpleTIFF(binary.LittleEndian, jpegBlob(0x08, 64)),
		"tiff-be": simpleTIFF(binary.BigEndian, jpegBlob(0x09, 64)),
		"raf":     rafFile(jpegBlob(0x0A, 96)),
		"exif-le": jpegWithEXIFThumb(binary.LittleEndian, jpegBlob(0x0B, 80)),
		"exif-be": jpegWithEXIFThumb(binary.BigEndian, jpegBlob(0x0C, 80)),
		"subifd":  subIFDFixture(),
		"chained": chainedFixture(),
	}
}

func subIFDFixture() []byte {
	b := newTIFF(binary.LittleEndian)
	payload := jpegBlob(0x0D, 128)
	off := b.blob(payload)
	sub := b.ifd(
		longEntry(tagStripOffsets, off),
		longEntry(tagStripByteCounts, uint32(len(payload))),
	)
	ifd0 := b.ifd(longEntry(tagSubIFDs, sub))
	b.setIFD0(ifd0)
	return b.bytes()
}

func chainedFixture() []byte {
	b := newTIFF(binary.BigEndian)
	small := jpegBlob(0x0E, 32)
	large := jpegBlob(0x0F, 320)
	smallOff := b.blob(small)
	largeOff := b.blob(large)
	ifd1 := b.ifd(
		longEntry(tagJPEGOffset, largeOff),
		longEntry(tagJPEGLength, uint32(len(large))),
	)
	ifd0 := b.ifd(
		longEntry(tagStripOffsets, smallOff),
		longEntry(tagStripByteCounts, uint32(len(small))),
	)
	b.setNext(ifd0, ifd1)
	b.setIFD0(ifd0)
	return b.bytes()
}

// TestTruncationNeverPanics truncates every fixture at every byte boundary and
// at every single-byte corruption, and requires both extractors to survive.
func TestTruncationNeverPanics(t *testing.T) {
	for name, data := range fixtures() {
		t.Run(name, func(t *testing.T) {
			for i := 0; i <= len(data); i++ {
				checkNoPanic(t, data[:i:i])
			}
			for i := 0; i < len(data); i++ {
				corrupt := bytes.Clone(data)
				corrupt[i] = 0xFF
				checkNoPanic(t, corrupt)
				corrupt[i] = 0x00
				checkNoPanic(t, corrupt)
			}
		})
	}
}

// checkNoPanic runs both extractors and validates whatever they hand back.
func checkNoPanic(t *testing.T, data []byte) {
	t.Helper()
	for _, fn := range []struct {
		name string
		call func([]byte) ([]byte, error)
	}{
		{"ExtractLargestJPEG", ExtractLargestJPEG},
		{"ExtractEXIFThumb", ExtractEXIFThumb},
	} {
		out, err := fn.call(data)
		switch {
		case err != nil && out != nil:
			t.Fatalf("%s: returned %d bytes alongside error %v", fn.name, len(out), err)
		case err == nil && !validJPEG(out):
			t.Fatalf("%s: returned %d bytes that are not a JPEG", fn.name, len(out))
		}
	}
}

func validJPEG(b []byte) bool {
	return len(b) >= 4 && b[0] == 0xFF && b[1] == 0xD8
}

func FuzzExtractLargestJPEG(f *testing.F) {
	for _, data := range fixtures() {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte("FUJIFILMCCD-RAW "))
	f.Add([]byte("II*\x00\x08\x00\x00\x00"))
	f.Add([]byte("MM\x00*\x00\x00\x00\x08"))

	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := ExtractLargestJPEG(data)
		if err != nil {
			if out != nil {
				t.Fatalf("returned %d bytes alongside error %v", len(out), err)
			}
			return
		}
		if !validJPEG(out) {
			t.Fatalf("returned %d bytes that do not start with SOI", len(out))
		}
		if len(out) > len(data) {
			t.Fatalf("returned %d bytes from a %d byte input", len(out), len(data))
		}
	})
}

func FuzzExtractEXIFThumb(f *testing.F) {
	for _, data := range fixtures() {
		f.Add(data)
	}
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := ExtractEXIFThumb(data)
		if err != nil {
			if out != nil {
				t.Fatalf("returned %d bytes alongside error %v", len(out), err)
			}
			return
		}
		if !validJPEG(out) {
			t.Fatalf("returned %d bytes that do not start with SOI", len(out))
		}
		if len(out) > len(data) {
			t.Fatalf("returned %d bytes from a %d byte input", len(out), len(data))
		}
	})
}
