package exif

import (
	"encoding/binary"
)

// Builders for the containers the reader and the writer are fed. Real cameras
// are not available in a unit test, so every fixture is assembled here from
// the same primitives the parsers walk: a TIFF header, blobs of value bytes,
// and IFDs pointing at them.

type tiffBuilder struct {
	order binary.ByteOrder
	buf   []byte
}

func newTIFF(order binary.ByteOrder) *tiffBuilder {
	b := &tiffBuilder{order: order, buf: make([]byte, tiffHeaderSize)}
	if order == binary.LittleEndian {
		copy(b.buf, "II")
	} else {
		copy(b.buf, "MM")
	}
	order.PutUint16(b.buf[2:], 42)
	return b
}

// blob appends value bytes at an even offset and returns where they landed.
func (b *tiffBuilder) blob(p []byte) uint32 {
	if len(b.buf)%2 == 1 {
		b.buf = append(b.buf, 0)
	}
	off := uint32(len(b.buf))
	b.buf = append(b.buf, p...)
	return off
}

// tag is one IFD entry before it knows where its value will live.
type tag struct {
	id    uint16
	typ   uint16
	count uint32
	data  []byte
}

func ascii(id uint16, s string) tag {
	v := append([]byte(s), 0)
	return tag{id: id, typ: typeASCII, count: uint32(len(v)), data: v}
}

func (b *tiffBuilder) short(id uint16, vals ...uint16) tag {
	data := make([]byte, 2*len(vals))
	for i, v := range vals {
		b.order.PutUint16(data[2*i:], v)
	}
	return tag{id: id, typ: typeShort, count: uint32(len(vals)), data: data}
}

func (b *tiffBuilder) long(id uint16, vals ...uint32) tag {
	data := make([]byte, 4*len(vals))
	for i, v := range vals {
		b.order.PutUint32(data[4*i:], v)
	}
	return tag{id: id, typ: typeLong, count: uint32(len(vals)), data: data}
}

func (b *tiffBuilder) rational(id uint16, pairs ...[2]uint32) tag {
	data := make([]byte, 8*len(pairs))
	for i, p := range pairs {
		b.order.PutUint32(data[8*i:], p[0])
		b.order.PutUint32(data[8*i+4:], p[1])
	}
	return tag{id: id, typ: typeRational, count: uint32(len(pairs)), data: data}
}

func bytes1(id uint16, vals ...byte) tag {
	return tag{id: id, typ: typeByte, count: uint32(len(vals)), data: append([]byte(nil), vals...)}
}

// undefined is a tag of the opaque UNDEFINED type, which is what a MakerNote
// is: bytes whose meaning the container does not describe.
func undefined(id uint16, data []byte) tag {
	return tag{id: id, typ: typeUndefined, count: uint32(len(data)), data: append([]byte(nil), data...)}
}

// ifd writes the entries as an IFD and returns its offset. Values wider than
// the 4-byte inline slot are blobbed first, so the IFD itself stays one
// contiguous run of 12-byte entries.
func (b *tiffBuilder) ifd(entries []tag, next uint32) uint32 {
	offsets := make([]uint32, len(entries))
	for i, e := range entries {
		if len(e.data) > 4 {
			offsets[i] = b.blob(e.data)
		}
	}
	if len(b.buf)%2 == 1 {
		b.buf = append(b.buf, 0)
	}
	at := uint32(len(b.buf))

	block := make([]byte, 2+len(entries)*ifdEntrySize+4)
	b.order.PutUint16(block, uint16(len(entries)))
	for i, e := range entries {
		p := block[2+i*ifdEntrySize:]
		b.order.PutUint16(p[0:], e.id)
		b.order.PutUint16(p[2:], e.typ)
		b.order.PutUint32(p[4:], e.count)
		if len(e.data) > 4 {
			b.order.PutUint32(p[8:], offsets[i])
		} else {
			copy(p[8:12], e.data)
		}
	}
	b.order.PutUint32(block[2+len(entries)*ifdEntrySize:], next)
	b.buf = append(b.buf, block...)
	return at
}

// done points the header at IFD0 and returns the finished TIFF block.
func (b *tiffBuilder) done(ifd0 uint32) []byte {
	b.order.PutUint32(b.buf[4:], ifd0)
	return b.buf
}

// fullTIFF builds a TIFF block carrying every field the reader extracts, in
// the layout a camera writes: IFD0 with the body tags and pointers, a private
// EXIF IFD, and a private GPS IFD.
func fullTIFF(order binary.ByteOrder) []byte {
	b := newTIFF(order)
	gps := b.ifd([]tag{
		ascii(tagGPSLatitudeRef, "N"),
		b.rational(tagGPSLatitude, [2]uint32{51, 1}, [2]uint32{30, 1}, [2]uint32{2400, 100}),
		ascii(tagGPSLongitudeRef, "W"),
		b.rational(tagGPSLongitude, [2]uint32{0, 1}, [2]uint32{7, 1}, [2]uint32{3900, 100}),
		bytes1(tagGPSAltitudeRef, 0),
		b.rational(tagGPSAltitude, [2]uint32{3550, 100}),
	}, 0)
	exif := b.ifd([]tag{
		b.rational(tagExposureTime, [2]uint32{1, 250}),
		b.rational(tagFNumber, [2]uint32{28, 10}),
		b.short(tagISOSpeedRatings, 640),
		ascii(tagDateTimeOriginal, "2026:08:03 19:42:07"),
		ascii(tagOffsetTimeOriginal, "+02:00"),
		ascii(tagSubSecTimeOriginal, "37"),
		b.rational(tagFocalLength, [2]uint32{35, 1}),
		b.long(tagPixelXDimension, 6240),
		b.long(tagPixelYDimension, 4160),
		undefined(tagMakerNote, []byte("MAKERNOTE-PRIVATE-BYTES")),
		ascii(tagLensModel, "XF35mmF1.4 R"),
	}, 0)
	ifd0 := b.ifd([]tag{
		b.short(tagOrientation, 6),
		ascii(tagMake, "FUJIFILM"),
		ascii(tagModel, "X-T5"),
		ascii(tagArtist, "Old Artist"),
		ascii(tagCopyright, "Old Copyright"),
		b.long(tagExifIFD, exif),
		b.long(tagGPSIFD, gps),
	}, 0)
	return b.done(ifd0)
}

// jpegWith wraps a TIFF block as the EXIF APP1 of an otherwise complete JPEG,
// with an ICC profile segment and an entropy-coded stream either side of it so
// a rewrite can be checked for leaving them alone.
func jpegWith(tiff []byte) []byte {
	out := []byte{0xFF, 0xD8}
	out = append(out, segment(0xE0, append([]byte("JFIF\x00"), 1, 2, 0, 0, 1, 0, 1, 0, 0))...)
	if tiff != nil {
		out = append(out, segment(0xE1, append([]byte(exifHeader), tiff...))...)
	}
	out = append(out, segment(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), make([]byte, 128)...))...)
	out = append(out, segment(0xDB, make([]byte, 65))...)
	out = append(out, segment(0xC0, []byte{8, 0, 16, 0, 16, 1, 0x11, 0})...)
	out = append(out, segment(0xC4, make([]byte, 29))...)
	out = append(out, segment(0xDA, []byte{1, 0, 0, 0, 63, 0})...)
	out = append(out, 0x12, 0x34, 0x56, 0x78, 0x9A) // entropy-coded data
	out = append(out, 0xFF, 0xD9)
	return out
}

func segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}
