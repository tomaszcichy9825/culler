package exif

import (
	"encoding/binary"
	"math"
)

// The IFD types, and the width of one value of each. Types this package cannot
// make sense of have width 0 and yield no values at all.
const (
	typeByte      = 1
	typeASCII     = 2
	typeShort     = 3
	typeLong      = 4
	typeRational  = 5
	typeSByte     = 6
	typeUndefined = 7
	typeSShort    = 8
	typeSLong     = 9
	typeSRational = 10
	typeFloat     = 11
	typeDouble    = 12
)

func typeSize(typ uint16) uint32 {
	switch typ {
	case typeByte, typeASCII, typeSByte, typeUndefined:
		return 1
	case typeShort, typeSShort:
		return 2
	case typeLong, typeSLong, typeFloat:
		return 4
	case typeRational, typeSRational, typeDouble:
		return 8
	}
	return 0
}

// Limits on a hostile container, in the spirit of internal/preview: a real
// file uses a handful of IFDs with tens of entries each, so anything past
// these numbers is corruption rather than photography.
const (
	maxIFDEntries = 4096
	maxValues     = 4096
	maxASCII      = 4096

	tiffHeaderSize = 8
	ifdEntrySize   = 12
)

// reader holds a buffer whose byte 0 is the TIFF header. Every offset in the
// container is relative to that byte, including the ones inside an APP1
// segment, so one reader serves both entry points.
type reader struct {
	buf   []byte
	order binary.ByteOrder
}

// entry is one 12-byte IFD entry with its value already located: either the
// four bytes packed into the entry itself, or a span elsewhere in the buffer.
// Locating it once here is what keeps every accessor free of bounds checks
// that could be forgotten.
type entry struct {
	tag   uint16
	typ   uint16
	count uint32
	// value is the entry's own four bytes, kept for the writer, which
	// reproduces entries it does not understand verbatim.
	value [4]byte
	// span is the value bytes wherever they actually live, or nil when the
	// entry's type, count or offset does not describe anything readable.
	span []byte
	// inline records whether span points into the entry rather than the file,
	// which the writer needs to know before it moves anything.
	inline bool
	// at is the offset span begins at, meaningful only when inline is false.
	at uint32
}

// directory is one IFD: its entries by tag, in file order, plus where it sat
// and what it pointed at next.
type directory struct {
	entries []entry
	byTag   map[uint16]entry
	at      uint32
	next    uint32
}

func (d *directory) get(tag uint16) (entry, bool) {
	if d == nil {
		return entry{}, false
	}
	e, ok := d.byTag[tag]
	return e, ok
}

// newReader validates the 8-byte TIFF header and returns the reader plus the
// offset of IFD0.
func newReader(buf []byte) (*reader, uint32, bool) {
	if len(buf) < tiffHeaderSize {
		return nil, 0, false
	}
	var order binary.ByteOrder
	switch {
	case buf[0] == 'I' && buf[1] == 'I':
		order = binary.LittleEndian
	case buf[0] == 'M' && buf[1] == 'M':
		order = binary.BigEndian
	default:
		return nil, 0, false
	}
	// 42 is baseline TIFF; RW2 uses 0x55 and ORF puts 'OR' or 'SR' in the same
	// slot. All three are otherwise ordinary IFD containers.
	switch order.Uint16(buf[2:]) {
	case 42, 0x55, 0x4F52, 0x5352:
	default:
		return nil, 0, false
	}
	return &reader{buf: buf, order: order}, order.Uint32(buf[4:]), true
}

// readIFD parses the entries at off. A truncated IFD yields the entries that
// are wholly present and no next pointer, so a clipped file still gives up
// whatever metadata it can.
func (r *reader) readIFD(off uint32) (*directory, bool) {
	base := uint64(off)
	if base < tiffHeaderSize || base+2 > uint64(len(r.buf)) {
		return nil, false
	}
	count := uint64(r.order.Uint16(r.buf[base:]))
	if count == 0 || count > maxIFDEntries {
		return nil, false
	}
	avail := (uint64(len(r.buf)) - base - 2) / ifdEntrySize
	complete := count <= avail
	if !complete {
		count = avail
	}
	if count == 0 {
		return nil, false
	}

	d := &directory{
		entries: make([]entry, 0, count),
		byTag:   make(map[uint16]entry, count),
		at:      off,
	}
	for i := uint64(0); i < count; i++ {
		p := r.buf[base+2+i*ifdEntrySize:]
		e := entry{
			tag:   r.order.Uint16(p[0:]),
			typ:   r.order.Uint16(p[2:]),
			count: r.order.Uint32(p[4:]),
		}
		copy(e.value[:], p[8:12])
		r.locate(&e)
		d.entries = append(d.entries, e)
		// First writing of a tag wins, which is what every reader does with a
		// directory that repeats one.
		if _, seen := d.byTag[e.tag]; !seen {
			d.byTag[e.tag] = e
		}
	}
	if complete {
		if end := base + 2 + count*ifdEntrySize; end+4 <= uint64(len(r.buf)) {
			d.next = r.order.Uint32(r.buf[end:])
		}
	}
	return d, true
}

// locate fills in an entry's value span, leaving it nil for anything that does
// not fit inside the buffer.
func (r *reader) locate(e *entry) {
	size := typeSize(e.typ)
	if size == 0 || e.count == 0 || e.count > maxValues {
		return
	}
	total := uint64(size) * uint64(e.count)
	if total <= 4 {
		e.span = e.value[:total]
		e.inline = true
		return
	}
	at := uint64(r.order.Uint32(e.value[:]))
	if at < tiffHeaderSize || at+total > uint64(len(r.buf)) {
		return
	}
	e.span = r.buf[at : at+total]
	e.at = uint32(at)
}

// text decodes an ASCII entry, stopping at the first NUL and trimming the
// padding spaces some bodies write. An entry of any other type has no text.
func (r *reader) text(e entry) (string, bool) {
	if e.typ != typeASCII || e.span == nil || len(e.span) > maxASCII {
		return "", false
	}
	b := e.span
	for i, c := range b {
		if c == 0 {
			b = b[:i]
			break
		}
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return "", false
	}
	return string(b), true
}

// ints decodes the integer types. Signed types are sign-extended, so a
// negative altitude reference or exposure bias reads back as written.
func (r *reader) ints(e entry) []int64 {
	size := typeSize(e.typ)
	if e.span == nil || size == 0 || size > 4 {
		return nil
	}
	out := make([]int64, 0, e.count)
	for i := uint32(0); i < e.count; i++ {
		p := e.span[uint64(i)*uint64(size):]
		switch e.typ {
		case typeByte, typeUndefined, typeASCII:
			out = append(out, int64(p[0]))
		case typeSByte:
			out = append(out, int64(int8(p[0])))
		case typeShort:
			out = append(out, int64(r.order.Uint16(p)))
		case typeSShort:
			out = append(out, int64(int16(r.order.Uint16(p))))
		case typeLong:
			out = append(out, int64(r.order.Uint32(p)))
		case typeSLong:
			out = append(out, int64(int32(r.order.Uint32(p))))
		default:
			return nil
		}
	}
	return out
}

// rationals decodes the two rational types into numerator/denominator pairs.
func (r *reader) rationals(e entry) []Rational {
	if e.span == nil || (e.typ != typeRational && e.typ != typeSRational) {
		return nil
	}
	out := make([]Rational, 0, e.count)
	for i := uint32(0); i < e.count; i++ {
		p := e.span[uint64(i)*8:]
		num, den := r.order.Uint32(p), r.order.Uint32(p[4:])
		rat := Rational{Num: int64(num), Den: int64(den), Present: true}
		if e.typ == typeSRational {
			rat.Num, rat.Den = int64(int32(num)), int64(int32(den))
		}
		out = append(out, rat)
	}
	return out
}

// pointer reads an entry that names another IFD. Camera bodies write these as
// LONG, but a SHORT pointer is legal and does occur.
func (r *reader) pointer(d *directory, tag uint16) uint32 {
	e, ok := d.get(tag)
	if !ok {
		return 0
	}
	vals := r.ints(e)
	if len(vals) == 0 || vals[0] <= 0 || vals[0] > math.MaxUint32 {
		return 0
	}
	return uint32(vals[0])
}
