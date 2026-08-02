// Package preview extracts embedded JPEG previews from RAW containers and
// EXIF thumbnails from JPEG files, so the grid can be filled without a
// demosaicer. Every parser here is fed whatever bytes happen to sit on a
// memory card: it bounds-checks each offset before slicing, caps every loop,
// returns an error for anything it cannot make sense of, and never panics or
// reads past the end of the input.
package preview

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// TIFF tags carrying either preview bytes or a pointer to another IFD.
const (
	tagStripOffsets    = 0x0111
	tagStripByteCounts = 0x0117
	tagJPEGOffset      = 0x0201 // JPEGInterchangeFormat
	tagJPEGLength      = 0x0202 // JPEGInterchangeFormatLength
	tagSubIFDs         = 0x014A
	tagEXIFIFD         = 0x8769
)

// Limits on a hostile container. A real RAW uses a handful of IFDs with tens
// of entries; anything past these numbers is corruption, not photography.
const (
	maxIFDs       = 32
	maxIFDEntries = 4096
	maxValues     = 4096
	maxQueued     = 128
	maxSegments   = 4096

	tiffHeaderSize = 8
	ifdEntrySize   = 12
	minJPEGSize    = 4 // SOI + EOI
)

// ErrUnsupported is returned when the input is not a container this package
// recognises.
var ErrUnsupported = errors.New("preview: unrecognised container format")

// ErrNoPreview is returned when the container parses but holds no usable
// embedded JPEG.
var ErrNoPreview = errors.New("preview: no embedded JPEG preview found")

// ErrNoThumbnail is returned when a JPEG holds no EXIF thumbnail.
var ErrNoThumbnail = errors.New("preview: no EXIF thumbnail found")

// ExtractLargestJPEG returns the largest embedded JPEG preview from a RAW
// container. Supports TIFF/IFD-based containers (CR2 NEF ARW DNG ORF PEF RW2
// SRW share this layout) and Fujifilm RAF. Returns an error for anything it
// cannot parse.
func ExtractLargestJPEG(data []byte) ([]byte, error) {
	if isRAF(data) {
		return extractRAF(data)
	}
	r, ifd0, ok := newTIFFReader(data)
	if !ok {
		return nil, ErrUnsupported
	}
	best := r.largestPreview(ifd0)
	if best == nil {
		return nil, ErrNoPreview
	}
	return bytes.Clone(best), nil
}

// ExtractEXIFThumb returns the small IFD1 thumbnail embedded in a JPEG's
// EXIF APP1 segment, for instant grid fill.
func ExtractEXIFThumb(jpegData []byte) ([]byte, error) {
	if !hasSOI(jpegData) {
		return nil, ErrUnsupported
	}
	for _, tiff := range exifSegments(jpegData) {
		if thumb := thumbFromTIFF(tiff); thumb != nil {
			return bytes.Clone(thumb), nil
		}
	}
	return nil, ErrNoThumbnail
}

// --- Fujifilm RAF ----------------------------------------------------------

// Real files carry "FUJIFILMCCD-RAW " at offset 0; the manufacturer prefix is
// enough to tell the container apart from everything else we handle.
const rafMagic = "FUJIFILM"

// Fixed header slots holding the embedded JPEG's position, both big-endian
// uint32 regardless of the sensor data's byte order.
const (
	rafPreviewOffsetPos = 84
	rafPreviewLengthPos = 88
)

func isRAF(data []byte) bool {
	return len(data) >= len(rafMagic) && string(data[:len(rafMagic)]) == rafMagic
}

func extractRAF(data []byte) ([]byte, error) {
	if len(data) < rafPreviewLengthPos+4 {
		return nil, ErrNoPreview
	}
	off := uint64(binary.BigEndian.Uint32(data[rafPreviewOffsetPos:]))
	length := uint64(binary.BigEndian.Uint32(data[rafPreviewLengthPos:]))
	if length < minJPEGSize || off+length > uint64(len(data)) {
		return nil, ErrNoPreview
	}
	preview := data[off : off+length]
	if !hasSOI(preview) {
		return nil, ErrNoPreview
	}
	return bytes.Clone(preview), nil
}

// --- TIFF/IFD --------------------------------------------------------------

// tiffReader holds a buffer whose byte 0 is the TIFF header. Every offset in
// the container is relative to that byte, including EXIF thumbnail offsets in
// an APP1 segment, so the same reader serves both entry points.
type tiffReader struct {
	buf   []byte
	order binary.ByteOrder
}

// entry is one raw 12-byte IFD entry. The value field is kept unparsed
// because small values are packed into it and their layout depends on both
// the type size and the byte order.
type entry struct {
	tag   uint16
	typ   uint16
	count uint32
	value [4]byte
}

// candidate is a byte range that might hold a JPEG.
type candidate struct {
	offset uint32
	length uint32
}

// newTIFFReader validates the 8-byte TIFF header and returns the reader plus
// the offset of IFD0.
func newTIFFReader(buf []byte) (*tiffReader, uint32, bool) {
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
	// 42 is baseline TIFF. RW2 uses 0x55, ORF uses 'OR' or 'SR' in the same
	// slot; both are otherwise ordinary IFD containers.
	switch order.Uint16(buf[2:]) {
	case 42, 0x55, 0x4F52, 0x5352:
	default:
		return nil, 0, false
	}
	return &tiffReader{buf: buf, order: order}, order.Uint32(buf[4:]), true
}

// largestPreview walks the IFD chain plus any SubIFDs and the EXIF IFD, and
// returns the largest candidate that actually starts with an SOI marker. The
// result aliases the reader's buffer; callers copy before handing it out.
func (r *tiffReader) largestPreview(first uint32) []byte {
	var best []byte
	queue := []uint32{first}
	seen := make(map[uint32]bool)

	for visited := 0; visited < maxIFDs && len(queue) > 0; {
		off := queue[0]
		queue = queue[1:]
		if off < tiffHeaderSize || seen[off] {
			continue
		}
		seen[off] = true
		visited++

		entries, next, ok := r.parseIFD(off)
		if !ok {
			continue
		}
		for _, c := range r.candidates(entries, true) {
			if b := r.jpegAt(c); len(b) > len(best) {
				best = b
			}
		}
		if next != 0 && len(queue) < maxQueued {
			queue = append(queue, next)
		}
		for _, e := range entries {
			if e.tag != tagSubIFDs && e.tag != tagEXIFIFD {
				continue
			}
			for _, v := range r.values(e) {
				if len(queue) >= maxQueued {
					break
				}
				queue = append(queue, v)
			}
		}
	}
	return best
}

// parseIFD reads the entries at off. A truncated IFD yields the entries that
// are wholly present and no next pointer, so a clipped file still gives up
// whatever preview it can.
func (r *tiffReader) parseIFD(off uint32) (entries []entry, next uint32, ok bool) {
	base := uint64(off)
	if base+2 > uint64(len(r.buf)) {
		return nil, 0, false
	}
	count := uint64(r.order.Uint16(r.buf[base:]))
	if count == 0 || count > maxIFDEntries {
		return nil, 0, false
	}

	avail := (uint64(len(r.buf)) - base - 2) / ifdEntrySize
	complete := count <= avail
	if !complete {
		count = avail
	}

	entries = make([]entry, 0, count)
	for i := uint64(0); i < count; i++ {
		p := r.buf[base+2+i*ifdEntrySize:]
		e := entry{
			tag:   r.order.Uint16(p[0:]),
			typ:   r.order.Uint16(p[2:]),
			count: r.order.Uint32(p[4:]),
		}
		copy(e.value[:], p[8:12])
		entries = append(entries, e)
	}

	if complete {
		if end := base + 2 + count*ifdEntrySize; end+4 <= uint64(len(r.buf)) {
			next = r.order.Uint32(r.buf[end:])
		}
	}
	return entries, next, true
}

// values decodes an entry's numeric values. Only the integer types that can
// hold an offset are decoded; anything else, or any count that will not fit
// the buffer, yields no values at all.
func (r *tiffReader) values(e entry) []uint32 {
	size := typeSize(e.typ)
	if size == 0 || e.count == 0 || e.count > maxValues {
		return nil
	}
	total := uint64(size) * uint64(e.count)

	src := e.value[:]
	if total > 4 {
		base := uint64(r.order.Uint32(e.value[:]))
		if base+total > uint64(len(r.buf)) {
			return nil
		}
		src = r.buf[base : base+total]
	}

	out := make([]uint32, 0, e.count)
	for i := uint64(0); i < uint64(e.count); i++ {
		out = append(out, r.uint(src[i*uint64(size):], size))
	}
	return out
}

// candidates pairs the offset and length tags of one IFD. Strip tags are the
// usual home of a RAW's full-size preview; the JPEGInterchangeFormat pair
// holds thumbnails and, on several bodies, the preview as well.
func (r *tiffReader) candidates(entries []entry, includeStrips bool) []candidate {
	var stripOffsets, stripLengths, jpegOffsets, jpegLengths []uint32
	for _, e := range entries {
		switch e.tag {
		case tagStripOffsets:
			if includeStrips {
				stripOffsets = r.values(e)
			}
		case tagStripByteCounts:
			if includeStrips {
				stripLengths = r.values(e)
			}
		case tagJPEGOffset:
			jpegOffsets = r.values(e)
		case tagJPEGLength:
			jpegLengths = r.values(e)
		}
	}
	return append(mergeStrips(stripOffsets, stripLengths), pairUp(jpegOffsets, jpegLengths)...)
}

// mergeStrips folds a strip set into whole-image candidates. A preview split
// across strips is one JPEG: treating each strip as its own candidate would
// pass SOI validation on the first strip alone and serve the top band of the
// image. Contiguous runs merge into a single candidate; a strip that does not
// continue the run starts a new one, so a non-contiguous tail is dropped
// rather than ever served truncated.
func mergeStrips(offsets, lengths []uint32) []candidate {
	n := min(len(offsets), len(lengths))
	var out []candidate
	for i := 0; i < n; {
		start := uint64(offsets[i])
		total := uint64(lengths[i])
		i++
		for i < n && uint64(offsets[i]) == start+total {
			total += uint64(lengths[i])
			i++
		}
		if i < n {
			// broken run: the remaining strips do not follow on, so this
			// candidate would be a partial image
			continue
		}
		if start+total <= uint64(^uint32(0)) {
			out = append(out, candidate{offset: uint32(start), length: uint32(total)})
		}
	}
	return out
}

// jpegAt returns the candidate's bytes if they lie inside the buffer and
// begin with an SOI marker, and nil otherwise.
func (r *tiffReader) jpegAt(c candidate) []byte {
	start := uint64(c.offset)
	end := start + uint64(c.length)
	if c.length < minJPEGSize || start < tiffHeaderSize || end > uint64(len(r.buf)) {
		return nil
	}
	b := r.buf[start:end]
	if !hasSOI(b) {
		return nil
	}
	return b
}

// uint reads one unsigned integer of size bytes from the front of p.
func (r *tiffReader) uint(p []byte, size int) uint32 {
	switch size {
	case 1:
		return uint32(p[0])
	case 2:
		return uint32(r.order.Uint16(p))
	default:
		return r.order.Uint32(p)
	}
}

// typeSize returns the byte width of the IFD types that can carry an offset.
// Types wider than a uint32, and the string and float types, return 0.
func typeSize(typ uint16) int {
	switch typ {
	case 1, 6: // BYTE, SBYTE
		return 1
	case 3, 8: // SHORT, SSHORT
		return 2
	case 4, 9: // LONG, SLONG
		return 4
	}
	return 0
}

// pairUp zips offsets with lengths, ignoring any unmatched tail.
func pairUp(offsets, lengths []uint32) []candidate {
	n := min(len(offsets), len(lengths))
	out := make([]candidate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, candidate{offset: offsets[i], length: lengths[i]})
	}
	return out
}

// --- EXIF thumbnail --------------------------------------------------------

// exifSegments returns the TIFF blocks of every APP1 EXIF segment in a JPEG,
// in file order. Each block starts at its own TIFF header and stops at the
// end of its segment, so a thumbnail pointer can never reach outside it.
func exifSegments(data []byte) [][]byte {
	const exifHeader = "Exif\x00\x00"
	var out [][]byte

	for i, segments := 2, 0; i+4 <= len(data) && segments < maxSegments; {
		if data[i] != 0xFF {
			break // lost the marker structure, stop rather than guess
		}
		marker := data[i+1]
		switch {
		case marker == 0xFF:
			i++ // fill byte before the real marker
			continue
		case marker == 0x01 || marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2 // standalone marker, no payload
			continue
		case marker == 0xDA || marker == 0xD9:
			return out // entropy-coded data or end of image; no metadata past here
		}

		length := int(binary.BigEndian.Uint16(data[i+2:]))
		if length < 2 || i+2+length > len(data) {
			break
		}
		payload := data[i+4 : i+2+length]
		if marker == 0xE1 && len(payload) >= len(exifHeader) && string(payload[:len(exifHeader)]) == exifHeader {
			out = append(out, payload[len(exifHeader):])
		}
		i += 2 + length
		segments++
	}
	return out
}

// thumbFromTIFF returns the largest JPEG thumbnail found in IFD1 onwards of
// an EXIF block. IFD0 describes the full-size image and is skipped. The
// result aliases tiff; callers copy before handing it out.
func thumbFromTIFF(tiff []byte) []byte {
	r, off, ok := newTIFFReader(tiff)
	if !ok {
		return nil
	}
	var best []byte
	seen := make(map[uint32]bool)

	for n := 0; n < maxIFDs && off >= tiffHeaderSize && !seen[off]; n++ {
		seen[off] = true
		entries, next, ok := r.parseIFD(off)
		if !ok {
			break
		}
		if n > 0 {
			for _, c := range r.candidates(entries, false) {
				if b := r.jpegAt(c); len(b) > len(best) {
					best = b
				}
			}
		}
		off = next
	}
	return best
}

// hasSOI reports whether b is long enough to be a JPEG and starts with the
// start-of-image marker.
func hasSOI(b []byte) bool {
	return len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8
}
