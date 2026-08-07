package exif

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Changes is an edit to a frame's metadata. A nil pointer leaves the tag
// exactly as it is; a pointer to an empty string removes it. That distinction
// is the whole of the batch editor's rule — "a value you type replaces every
// frame, empty leaves them alone" — so it is modelled here rather than guessed
// at by each caller.
//
// Writing DateTimeOriginal also writes or removes the two tags that qualify
// it: OffsetTimeOriginal follows the CaptureTime's own rule, and
// SubSecTimeOriginal is written or cleared to match the fraction — a
// sub-second left behind from the old value would silently qualify the new
// one.
type Changes struct {
	DateTimeOriginal *CaptureTime
	Artist           *string
	Copyright        *string
	StripGPS         bool
	// SetGPS writes a location onto the frame, creating the GPS directory if it
	// has none. It wins over StripGPS: a caller that asked for both meant to set
	// the location it just chose, and losing that is the worse surprise.
	SetGPS *GPSCoord
}

// CaptureTime is a value for DateTimeOriginal. HasOffset says whether the
// time's zone is a recorded fact. When it is, OffsetTimeOriginal is written
// from Value's own zone; when it is not — a frame whose file never carried an
// offset, edited by a user who did not state one — no offset tag is written
// and any OffsetTimeOriginal already in the file is removed, because an old
// zone would qualify the new time with a fact nobody stated. The reader keeps
// the same distinction the other way round, so a zone-unknown time survives a
// round trip as exactly that.
type CaptureTime struct {
	Value     time.Time
	HasOffset bool
}

// GPSCoord is a location to write: decimal degrees, signed by hemisphere, with
// an altitude in metres that is negative below sea level and only meant when
// HasAltitude says so.
type GPSCoord struct {
	Latitude    float64
	Longitude   float64
	Altitude    float64
	HasAltitude bool
}

// Empty reports whether there is nothing to do. Clearing a tag is a change.
func (c Changes) Empty() bool {
	return c.DateTimeOriginal == nil && c.Artist == nil && c.Copyright == nil && !c.StripGPS && c.SetGPS == nil
}

// The most an APP1 segment can hold: its length field is two bytes and counts
// itself. A rewrite that would overflow it is refused rather than truncated.
const maxAPP1Payload = 0xFFFF - 2

var (
	errNotJPEG   = errors.New("exif: not a JPEG")
	errNoSegment = errors.New("exif: this JPEG carries no EXIF segment to write into")
	errBadTIFF   = errors.New("exif: the EXIF segment is not a readable TIFF block")
	errTooLarge  = errors.New("exif: the edited metadata no longer fits an APP1 segment")
	errEmptyIFD  = errors.New("exif: this edit would leave a metadata directory with no entries, which no reader accepts")
)

// RewriteJPEG returns data with its EXIF APP1 segment edited and every other
// byte of the file reproduced exactly. See the package documentation for why
// the TIFF block is patched rather than re-serialised.
func RewriteJPEG(data []byte, c Changes) ([]byte, error) {
	if c.Empty() {
		return bytes.Clone(data), nil
	}
	if !hasSOI(data) {
		return nil, errNotJPEG
	}
	var seg jpegSegment
	found := false
	for _, s := range segments(data) {
		if s.marker == 0xE1 && len(s.payload) > len(exifHeader) &&
			string(s.payload[:len(exifHeader)]) == exifHeader {
			seg, found = s, true
			break
		}
	}
	if !found {
		return nil, errNoSegment
	}

	block, err := editTIFF(seg.payload[len(exifHeader):], c)
	if err != nil {
		return nil, err
	}
	payload := len(exifHeader) + len(block)
	if payload > maxAPP1Payload {
		return nil, errTooLarge
	}

	out := make([]byte, 0, len(data)-(seg.end-seg.at)+payload+4)
	out = append(out, data[:seg.at]...)
	out = append(out, segmentHeader(0xE1, payload)...)
	out = append(out, exifHeader...)
	out = append(out, block...)
	return append(out, data[seg.end:]...), nil
}

func segmentHeader(marker byte, payload int) []byte {
	n := payload + 2
	return []byte{0xFF, marker, byte(n >> 8), byte(n)}
}

// WriteJPEG applies the changes to the file at path, replacing it atomically:
// the edited bytes are written to a temporary file beside it, synced, and
// renamed over the original, so a crash mid-write leaves either the old file
// or the new one and never half of either. The file's permissions survive.
//
// The application does not call this on a photograph sitting on a memory card
// — nothing may be written to the source, temporary file included. ExifService
// stages the same bytes in the app data directory and lets the op engine move
// them into place, which is what makes the write undoable as well as atomic.
func WriteJPEG(path string, c Changes) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := RewriteJPEG(data, c)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return replaceAtomically(path, out, info.Mode().Perm())
}

// replaceAtomically writes data over path via a temporary file in the same
// directory, which is what makes the rename a rename rather than a copy.
func replaceAtomically(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename has succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// WriteFile writes bytes to path atomically with the given permissions. It is
// how the service stages an edited frame before the op engine moves it.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return replaceAtomically(path, data, mode)
}

// --- the TIFF editor --------------------------------------------------------

// change is one tag edit inside an IFD. del removes the tag; otherwise data is
// the serialised value.
type change struct {
	tag   uint16
	typ   uint16
	count uint32
	data  []byte
	del   bool
}

// editor owns a mutable copy of a TIFF block. Every directory is re-read from
// the buffer at the moment it is needed rather than held across an append,
// because appending can move the backing array and leave a stale slice behind.
type editor struct {
	buf   []byte
	order binary.ByteOrder
	// ifd0 is where the first directory currently sits. Rebuilding it moves
	// it, and the header pointer follows in one place rather than at each
	// call site.
	ifd0 uint32
}

func editTIFF(block []byte, c Changes) ([]byte, error) {
	r, ifd0Off, ok := newReader(block)
	if !ok {
		return nil, errBadTIFF
	}
	ed := &editor{buf: bytes.Clone(block), order: r.order, ifd0: ifd0Off}
	if _, ok := ed.read(ed.ifd0); !ok {
		return nil, errBadTIFF
	}

	// The EXIF IFD first: relocating it repoints an entry in IFD0, which has
	// to happen before IFD0 is itself rebuilt.
	if c.DateTimeOriginal != nil {
		if err := ed.applyTimes(*c.DateTimeOriginal); err != nil {
			return nil, err
		}
	}

	var changes []change
	if c.Artist != nil {
		changes = append(changes, asciiChange(tagArtist, *c.Artist))
	}
	if c.Copyright != nil {
		changes = append(changes, asciiChange(tagCopyright, *c.Copyright))
	}
	switch {
	case c.SetGPS != nil:
		// Setting a location wins over stripping it. Done before IFD0 is rebuilt
		// below, because creating the GPS directory repoints an entry in IFD0.
		if err := ed.applyGPS(*c.SetGPS); err != nil {
			return nil, err
		}
	case c.StripGPS:
		ed.eraseGPS()
		changes = append(changes, change{tag: tagGPSIFD, del: true})
	}
	if err := ed.editIFD0(changes); err != nil {
		return nil, err
	}
	return ed.buf, nil
}

// applyGPS writes a location into the GPS IFD, creating that directory and
// pointing IFD0 at it when the frame has none — the same shape of change
// applyTimes makes for the EXIF IFD.
func (ed *editor) applyGPS(g GPSCoord) error {
	changes := gpsChanges(ed.order, g)

	d, ok := ed.read(ed.ifd0)
	if !ok {
		return errBadTIFF
	}
	gpsOff := ed.pointerOf(d, tagGPSIFD)
	if gpsOff == 0 {
		created, err := ed.newIFD(nil, additionsOnly(changes), 0)
		if err != nil {
			return err
		}
		return ed.editIFD0([]change{ed.longChange(tagGPSIFD, created)})
	}

	moved, err := ed.applyTo(gpsOff, changes)
	if err != nil {
		return err
	}
	if moved != gpsOff {
		return ed.editIFD0([]change{ed.longChange(tagGPSIFD, moved)})
	}
	return nil
}

// editIFD0 applies changes to the first directory and keeps the TIFF header's
// pointer in step when the directory has to be rebuilt somewhere else.
func (ed *editor) editIFD0(changes []change) error {
	if len(changes) == 0 {
		return nil
	}
	moved, err := ed.applyTo(ed.ifd0, changes)
	if err != nil {
		return err
	}
	ed.ifd0 = moved
	ed.order.PutUint32(ed.buf[4:], moved)
	return nil
}

// applyTimes writes DateTimeOriginal and the two tags that qualify it into the
// EXIF IFD, creating that directory if the file has none.
func (ed *editor) applyTimes(when CaptureTime) error {
	changes := []change{
		asciiChange(tagDateTimeOriginal, when.Value.Format(exifTimeLayout)),
	}
	if when.HasOffset {
		changes = append(changes, asciiChange(tagOffsetTimeOriginal, formatOffset(when.Value)))
	} else {
		// The zone is not known, so no offset is written — and an offset left
		// over from the old time would qualify the new one with a fact nobody
		// stated, the same hazard as a stale sub-second.
		changes = append(changes, change{tag: tagOffsetTimeOriginal, del: true})
	}
	if sub := formatSubSec(when.Value); sub != "" {
		changes = append(changes, asciiChange(tagSubSecTimeOriginal, sub))
	} else {
		// A sub-second left over from the old value would qualify the new one.
		changes = append(changes, change{tag: tagSubSecTimeOriginal, del: true})
	}

	d, ok := ed.read(ed.ifd0)
	if !ok {
		return errBadTIFF
	}
	exifOff := ed.pointerOf(d, tagExifIFD)
	if exifOff == 0 {
		created, err := ed.newIFD(nil, additionsOnly(changes), 0)
		if err != nil {
			return err
		}
		return ed.editIFD0([]change{ed.longChange(tagExifIFD, created)})
	}

	moved, err := ed.applyTo(exifOff, changes)
	if err != nil {
		return err
	}
	if moved != exifOff {
		return ed.editIFD0([]change{ed.longChange(tagExifIFD, moved)})
	}
	return nil
}

// additionsOnly drops the deletions from a set of changes. A directory being
// created from nothing has nothing to delete, and a deletion serialised anyway
// would land as a type-0, count-0 entry a strict reader treats as corruption.
// applyTo filters deletions itself; this is for the call sites that hand a raw
// change set straight to newIFD.
func additionsOnly(changes []change) []change {
	out := make([]change, 0, len(changes))
	for _, ch := range changes {
		if !ch.del {
			out = append(out, ch)
		}
	}
	return out
}

// applyTo edits the IFD at off and returns where it now lives. An edit that
// only changes values patches them where they sit; an edit that adds or
// removes an entry rebuilds the directory at the end of the block, copying
// every other entry verbatim so the values they point at never move.
func (ed *editor) applyTo(off uint32, changes []change) (uint32, error) {
	d, ok := ed.read(off)
	if !ok {
		return off, errBadTIFF
	}

	var structural []change
	for _, ch := range changes {
		_, exists := d.byTag[ch.tag]
		switch {
		case ch.del && !exists:
			// nothing to remove
		case ch.del || !exists:
			structural = append(structural, ch)
		default:
			if err := ed.patch(off, ch); err != nil {
				return off, err
			}
		}
	}
	if len(structural) == 0 {
		return off, nil
	}

	// Re-read: the patches above rewrote entry bytes in this very directory.
	d, ok = ed.read(off)
	if !ok {
		return off, errBadTIFF
	}
	drop := make(map[uint16]bool, len(structural))
	var add []change
	for _, ch := range structural {
		if ch.del {
			drop[ch.tag] = true
			continue
		}
		add = append(add, ch)
	}
	var keep []entry
	var stale []span
	for _, e := range d.entries {
		if !drop[e.tag] {
			keep = append(keep, e)
			continue
		}
		if !e.inline && e.span != nil {
			stale = append(stale, span{at: e.at, length: uint32(len(e.span))})
		}
	}
	moved, err := ed.newIFD(keep, add, d.next)
	if err != nil {
		return off, err
	}
	// A deleted entry's out-of-line value gets the same treatment patch gives
	// a replaced one: once nothing else references those bytes they are
	// zeroed, not left readable in the file. The old directory is still
	// hooked up until the caller repoints it, so the walk skips exactly the
	// entries this rebuild dropped and sees reachability as it is about to be.
	if len(stale) > 0 {
		reach := ed.reachableSpans(func(dir uint32, e entry) bool {
			return dir == off && drop[e.tag]
		})
		for _, s := range stale {
			if !overlapsAny(s.at, s.length, reach) {
				ed.zero(s.at, s.length)
			}
		}
	}
	return moved, nil
}

// patch overwrites an existing entry's value. It writes into the old value's
// span when the new one fits, and otherwise appends the new value to the end
// of the block and repoints just this entry.
func (ed *editor) patch(ifdAt uint32, ch change) error {
	d, ok := ed.read(ifdAt)
	if !ok {
		return errBadTIFF
	}
	index := -1
	for i, e := range d.entries {
		if e.tag == ch.tag {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	old := d.entries[index]
	// The old value's out-of-line bytes, cleared below once nothing points at
	// them any more. A value that merely became unreachable would still be
	// sitting in the file for anyone with a hex editor — the same reasoning
	// that has eraseGPS zero the coordinates, applied to every replacement.
	var stale span
	if !old.inline && old.span != nil {
		stale = span{at: old.at, length: uint32(len(old.span))}
	}

	var inline [4]byte
	switch {
	case len(ch.data) <= 4:
		copy(inline[:], ch.data)
	case !old.inline && len(ch.data) <= len(old.span):
		at := old.at
		copy(ed.buf[at:], ch.data)
		// Anything the shorter value no longer covers is cleared rather than
		// left as the tail of the string that used to be there.
		for i := at + uint32(len(ch.data)); i < at+uint32(len(old.span)); i++ {
			ed.buf[i] = 0
		}
		ed.order.PutUint32(inline[:], at)
		stale = span{} // the old span now holds the new value
	default:
		at, err := ed.appendBlob(ch.data)
		if err != nil {
			return err
		}
		ed.order.PutUint32(inline[:], at)
	}

	// Appending may have moved the buffer, but never the directory, so the
	// entry's offset within the block is still right.
	p := ed.buf[uint64(ifdAt)+2+uint64(index)*ifdEntrySize:]
	ed.order.PutUint16(p[2:], ch.typ)
	ed.order.PutUint32(p[4:], ch.count)
	copy(p[8:12], inline[:])

	// With the entry repointed, the old span is unreachable unless a second
	// entry shares those bytes, in which case they are the survivor's to keep.
	if stale.length > 0 && !overlapsAny(stale.at, stale.length, ed.reachableSpans(nil)) {
		ed.zero(stale.at, stale.length)
	}
	return nil
}

// newIFD writes a directory at the end of the block: the entries to keep
// reproduced byte for byte, plus the additions, in ascending tag order.
func (ed *editor) newIFD(keep []entry, add []change, next uint32) (uint32, error) {
	type row struct {
		tag   uint16
		typ   uint16
		count uint32
		value [4]byte
	}
	rows := make([]row, 0, len(keep)+len(add))
	for _, e := range keep {
		rows = append(rows, row{tag: e.tag, typ: e.typ, count: e.count, value: e.value})
	}
	for _, ch := range add {
		r := row{tag: ch.tag, typ: ch.typ, count: ch.count}
		if len(ch.data) <= 4 {
			copy(r.value[:], ch.data)
		} else {
			at, err := ed.appendBlob(ch.data)
			if err != nil {
				return 0, err
			}
			ed.order.PutUint32(r.value[:], at)
		}
		rows = append(rows, r)
	}
	// A directory with no entries at all is one this package's own reader —
	// and most others — refuses as corrupt, so the edit that would produce
	// one is refused with the truth rather than blamed on the segment size.
	if len(rows) == 0 {
		return 0, errEmptyIFD
	}
	if len(rows) > maxIFDEntries {
		return 0, errTooLarge
	}
	// TIFF asks for entries in ascending tag order and some readers rely on it.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].tag < rows[j-1].tag; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}

	block := make([]byte, 2+len(rows)*ifdEntrySize+4)
	ed.order.PutUint16(block, uint16(len(rows)))
	for i, r := range rows {
		p := block[2+i*ifdEntrySize:]
		ed.order.PutUint16(p[0:], r.tag)
		ed.order.PutUint16(p[2:], r.typ)
		ed.order.PutUint32(p[4:], r.count)
		copy(p[8:12], r.value[:])
	}
	ed.order.PutUint32(block[2+len(rows)*ifdEntrySize:], next)
	return ed.appendBlob(block)
}

// appendBlob puts bytes at an even offset at the end of the block and returns
// where they landed. An APP1 segment cannot exceed 64 KB, so a block that
// grows past that is refused here rather than producing a wrapped length.
func (ed *editor) appendBlob(p []byte) (uint32, error) {
	if len(ed.buf)%2 == 1 {
		ed.buf = append(ed.buf, 0)
	}
	if len(ed.buf)+len(p) > maxAPP1Payload {
		return 0, errTooLarge
	}
	at := uint32(len(ed.buf))
	ed.buf = append(ed.buf, p...)
	return at, nil
}

// read parses the directory at off out of the current buffer.
func (ed *editor) read(off uint32) (*directory, bool) {
	return (&reader{buf: ed.buf, order: ed.order}).readIFD(off)
}

func (ed *editor) pointerOf(d *directory, tag uint16) uint32 {
	return (&reader{buf: ed.buf, order: ed.order}).pointer(d, tag)
}

// eraseGPS blanks the GPS directory and the values only it referenced. The
// pointer to it is removed separately; unhooking alone would leave the
// coordinates in the file for anyone willing to open it in a hex editor.
func (ed *editor) eraseGPS() {
	ifd0, ok := ed.read(ed.ifd0)
	if !ok {
		return
	}
	gpsOff := ed.pointerOf(ifd0, tagGPSIFD)
	if gpsOff == 0 {
		return
	}
	gps, ok := ed.read(gpsOff)
	if !ok {
		return
	}

	// Everything still reachable once the GPS pointer is unhooked from IFD0 —
	// only that one edge goes, so a directory the Exif pointer shares, or a
	// pointer aimed into the middle of a MakerNote, stays alive and untouched.
	keep := ed.reachableSpans(func(dir uint32, e entry) bool {
		return dir == ed.ifd0 && e.tag == tagGPSIFD
	})
	for _, e := range gps.entries {
		if e.inline || e.span == nil {
			continue
		}
		if overlapsAny(e.at, uint32(len(e.span)), keep) {
			continue
		}
		ed.zero(e.at, uint32(len(e.span)))
	}
	// The directory's own bytes get the same guard as its values: a span some
	// other chain still reaches belongs to the survivor, not to this erase.
	length := uint32(2 + len(gps.entries)*ifdEntrySize + 4)
	if !overlapsAny(gpsOff, length, keep) {
		ed.zero(gpsOff, length)
	}
}

// span is a byte range inside the block.
type span struct{ at, length uint32 }

// reachableSpans lists every value and directory the block reaches, skipping
// the entries except reports true for — the one pointer being unhooked, or
// the entries a rebuild is dropping — so the caller sees reachability as it
// will be once its own edit lands. Skipping an entry rather than a directory
// is what keeps an aliased target alive: a directory two pointers share is
// still reachable through the pointer that stays.
func (ed *editor) reachableSpans(except func(dir uint32, e entry) bool) []span {
	var out []span
	seen := map[uint32]bool{}
	queue := []uint32{ed.ifd0}

	for len(queue) > 0 && len(out) < maxValues {
		off := queue[0]
		queue = queue[1:]
		if off == 0 || seen[off] {
			continue
		}
		seen[off] = true
		d, ok := ed.read(off)
		if !ok {
			continue
		}
		out = append(out, span{at: off, length: uint32(2 + len(d.entries)*ifdEntrySize + 4)})
		for _, e := range d.entries {
			if except != nil && except(off, e) {
				continue
			}
			if !e.inline && e.span != nil {
				out = append(out, span{at: e.at, length: uint32(len(e.span))})
			}
			switch e.tag {
			case tagExifIFD, tagGPSIFD, tagInteropIFD:
				queue = append(queue, ed.pointerOf(d, e.tag))
			case tagSubIFDs:
				// One SubIFDs entry names a whole list of directories, and
				// every one of them keeps its bytes alive.
				for _, v := range (&reader{buf: ed.buf, order: ed.order}).ints(e) {
					if v > 0 && v <= math.MaxUint32 {
						queue = append(queue, uint32(v))
					}
				}
			}
		}
		if d.next != 0 {
			queue = append(queue, d.next)
		}
	}
	return out
}

func overlapsAny(at, length uint32, spans []span) bool {
	for _, s := range spans {
		if at < s.at+s.length && s.at < at+length {
			return true
		}
	}
	return false
}

func (ed *editor) zero(at, length uint32) {
	end := uint64(at) + uint64(length)
	if uint64(at) < tiffHeaderSize || end > uint64(len(ed.buf)) {
		return
	}
	for i := at; uint64(i) < end; i++ {
		ed.buf[i] = 0
	}
}

// --- value serialisation ----------------------------------------------------

// asciiChange builds an ASCII tag edit. An empty string removes the tag,
// because a zero-length string and no string at all are the same fact and only
// one of them can be written.
func asciiChange(tag uint16, value string) change {
	if value == "" {
		return change{tag: tag, del: true}
	}
	data := append([]byte(value), 0)
	return change{tag: tag, typ: typeASCII, count: uint32(len(data)), data: data}
}

// longChange builds a LONG tag edit in the block's own byte order. Four bytes
// fit the entry's inline slot, so it never needs a value block of its own.
func (ed *editor) longChange(tag uint16, value uint32) change {
	data := make([]byte, 4)
	ed.order.PutUint32(data, value)
	return change{tag: tag, typ: typeLong, count: 1, data: data}
}

// gpsChanges is the full set of tag edits for one location: the version, both
// coordinates with their hemisphere references, and the altitude when there is
// one. Producing them together lets applyTo rebuild the GPS IFD in one pass,
// whether the directory already existed or is being created from nothing.
func gpsChanges(order binary.ByteOrder, g GPSCoord) []change {
	latRef, lonRef := "N", "E"
	if g.Latitude < 0 {
		latRef = "S"
	}
	if g.Longitude < 0 {
		lonRef = "W"
	}

	changes := []change{
		// GPSVersionID 2.3.0.0, four BYTEs, inline. Cameras write it and some
		// strict readers expect the directory to carry it.
		{tag: tagGPSVersionID, typ: typeByte, count: 4, data: []byte{2, 3, 0, 0}},
		gpsRefChange(tagGPSLatitudeRef, latRef),
		coordChange(order, tagGPSLatitude, g.Latitude),
		gpsRefChange(tagGPSLongitudeRef, lonRef),
		coordChange(order, tagGPSLongitude, g.Longitude),
	}
	if g.HasAltitude {
		ref := byte(0)
		alt := g.Altitude
		if alt < 0 {
			ref, alt = 1, -alt
		}
		changes = append(changes,
			change{tag: tagGPSAltitudeRef, typ: typeByte, count: 1, data: []byte{ref}},
			change{tag: tagGPSAltitude, typ: typeRational, count: 1, data: rationalBytes(order, uint32(math.Round(alt*100)), 100)},
		)
	} else {
		// No altitude with the new position, so any altitude the frame already
		// carried must go — a leftover would silently qualify the new
		// coordinates, the same way a stale sub-second would a new capture time.
		changes = append(changes,
			change{tag: tagGPSAltitudeRef, del: true},
			change{tag: tagGPSAltitude, del: true},
		)
	}
	return changes
}

// gpsRefChange is a hemisphere reference: one letter and its null, a two-byte
// ASCII value that fits an entry inline.
func gpsRefChange(tag uint16, ref string) change {
	return change{tag: tag, typ: typeASCII, count: 2, data: append([]byte(ref), 0)}
}

// coordChange encodes decimal degrees as the three RATIONALs — degrees,
// minutes, seconds — a GPS coordinate is stored as. Degrees and minutes are
// whole; the seconds carry the remainder to ten-thousandths, finer than a
// tenth of a metre and so finer than a dropped pin or a camera ever means.
func coordChange(order binary.ByteOrder, tag uint16, deg float64) change {
	deg = math.Abs(deg)
	d := math.Floor(deg)
	remMin := (deg - d) * 60
	m := math.Floor(remMin)
	sec := (remMin - m) * 60
	const secDen = 10000

	data := make([]byte, 0, 24)
	data = append(data, rationalBytes(order, uint32(d), 1)...)
	data = append(data, rationalBytes(order, uint32(m), 1)...)
	data = append(data, rationalBytes(order, uint32(math.Round(sec*secDen)), secDen)...)
	return change{tag: tag, typ: typeRational, count: 3, data: data}
}

// rationalBytes is one RATIONAL — numerator then denominator — in the block's
// byte order.
func rationalBytes(order binary.ByteOrder, num, den uint32) []byte {
	b := make([]byte, 8)
	order.PutUint32(b, num)
	order.PutUint32(b[4:], den)
	return b
}

// formatOffset renders a time's zone as the "+02:00" the offset tag holds.
func formatOffset(t time.Time) string {
	_, secs := t.Zone()
	sign := "+"
	if secs < 0 {
		sign, secs = "-", -secs
	}
	return fmt.Sprintf("%s%02d:%02d", sign, secs/3600, (secs%3600)/60)
}

// formatSubSec renders the fraction of a second as the digits the sub-second
// tag holds, or the empty string for a whole second.
func formatSubSec(t time.Time) string {
	ns := t.Nanosecond()
	if ns == 0 {
		return ""
	}
	s := strings.TrimRight(fmt.Sprintf("%06d", ns/1000), "0")
	if s == "" {
		return ""
	}
	return s
}

// --- XMP --------------------------------------------------------------------

// The namespaces the sidecar properties live in, and the RDF namespace whose
// Seq and Alt containers structure two of them.
const (
	nsDC       = "http://purl.org/dc/elements/1.1/"
	nsEXIF     = "http://ns.adobe.com/exif/1.0/"
	nsXMPBasic = "http://ns.adobe.com/xap/1.0/"
)

// XMPProperty is one property of a frame's sidecar this package writes: the
// namespace and name that identify it, and its content. The list a Changes
// produces is the single statement of what an edit means in XMP \u2014 RenderXMP
// serialises it into a fresh document, and internal/xmpexport splices it into
// a sidecar that already exists, so the two can never say different things.
type XMPProperty struct {
	// Space is the namespace URI, Local the property name within it.
	Space, Local string
	// Inner renders the element's content given the prefix the target
	// document binds the RDF namespace to \u2014 the one foreign namespace a value
	// nests, for the Seq and Alt containers. Nil removes the property and
	// writes nothing in its place.
	Inner func(rdfPrefix string) string
}

// XMPProperties is the sidecar rendering of the changes: only the properties
// this edit touches, so merging one edit into a sidecar leaves what an earlier
// edit \u2014 or another tool \u2014 put there.
func (c Changes) XMPProperties() []XMPProperty {
	text := func(s string) func(string) string {
		return func(string) string { return s }
	}
	var out []XMPProperty
	if c.DateTimeOriginal != nil {
		when := text(escapeXML(formatXMPTime(*c.DateTimeOriginal)))
		out = append(out,
			XMPProperty{Space: nsEXIF, Local: "DateTimeOriginal", Inner: when},
			XMPProperty{Space: nsXMPBasic, Local: "CreateDate", Inner: when},
		)
	}
	if c.Artist != nil {
		p := XMPProperty{Space: nsDC, Local: "creator"}
		if *c.Artist != "" {
			name := escapeXML(*c.Artist)
			p.Inner = func(rdf string) string {
				return fmt.Sprintf("<%s:Seq><%s:li>%s</%s:li></%s:Seq>", rdf, rdf, name, rdf, rdf)
			}
		}
		out = append(out, p)
	}
	if c.Copyright != nil {
		p := XMPProperty{Space: nsDC, Local: "rights"}
		if *c.Copyright != "" {
			rights := escapeXML(*c.Copyright)
			p.Inner = func(rdf string) string {
				return fmt.Sprintf(`<%s:Alt><%s:li xml:lang="x-default">%s</%s:li></%s:Alt>`, rdf, rdf, rights, rdf, rdf)
			}
		}
		out = append(out, p)
	}
	switch {
	case c.SetGPS != nil:
		// The sidecar is where a RAW frame's location lives: the RAW is never
		// rewritten, so the position it should carry is written beside it in the
		// form every other tool reads, "degrees,decimal-minutes" with the
		// hemisphere letter.
		out = append(out,
			XMPProperty{Space: nsEXIF, Local: "GPSLatitude", Inner: text(xmpCoord(c.SetGPS.Latitude, "N", "S"))},
			XMPProperty{Space: nsEXIF, Local: "GPSLongitude", Inner: text(xmpCoord(c.SetGPS.Longitude, "E", "W"))},
		)
		if c.SetGPS.HasAltitude {
			alt, ref := c.SetGPS.Altitude, "0"
			if alt < 0 {
				alt, ref = -alt, "1"
			}
			out = append(out,
				XMPProperty{Space: nsEXIF, Local: "GPSAltitude", Inner: text(fmt.Sprintf("%d/100", int64(math.Round(alt*100))))},
				XMPProperty{Space: nsEXIF, Local: "GPSAltitudeRef", Inner: text(ref)},
			)
		} else {
			// No altitude with the new position, so any altitude the sidecar
			// already carried must go \u2014 a leftover would silently qualify the
			// new coordinates.
			out = append(out,
				XMPProperty{Space: nsEXIF, Local: "GPSAltitude"},
				XMPProperty{Space: nsEXIF, Local: "GPSAltitudeRef"},
			)
		}
	case c.StripGPS:
		// A sidecar cannot remove what is inside the RAW, so it records the
		// intent as an empty position rather than claiming the file was edited.
		out = append(out,
			XMPProperty{Space: nsEXIF, Local: "GPSLatitude", Inner: text("")},
			XMPProperty{Space: nsEXIF, Local: "GPSLongitude", Inner: text("")},
		)
	}
	return out
}

// RenderXMP builds a minimal, well-formed XMP sidecar carrying the changes. It
// is what a RAW frame gets instead of a rewrite when no sidecar exists yet:
// the RAW itself is never opened for writing, so the edit lives beside it in a
// file every other tool already knows how to read. A sidecar that does exist
// is merged into instead \u2014 see internal/xmpexport.
//
// Only the fields this package writes appear. A sidecar is not a copy of the
// frame's metadata and does not pretend to be one.
func RenderXMP(c Changes) []byte {
	prefixes := map[string]string{nsDC: "dc", nsEXIF: "exif", nsXMPBasic: "xmp"}

	var b strings.Builder
	b.WriteString("<?xpacket begin=\"\uFEFF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="culler">` + "\n")
	b.WriteString(`  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	b.WriteString(`    <rdf:Description rdf:about=""` + "\n")
	b.WriteString(`        xmlns:dc="` + nsDC + `"` + "\n")
	b.WriteString(`        xmlns:exif="` + nsEXIF + `"` + "\n")
	b.WriteString(`        xmlns:xmp="` + nsXMPBasic + `">` + "\n")

	for _, p := range c.XMPProperties() {
		if p.Inner == nil {
			continue // a removal has nothing to remove from a fresh document
		}
		name := prefixes[p.Space] + ":" + p.Local
		b.WriteString("      <" + name + ">" + p.Inner("rdf") + "</" + name + ">\n")
	}

	b.WriteString("    </rdf:Description>\n  </rdf:RDF>\n</x:xmpmeta>\n")
	b.WriteString(`<?xpacket end="w"?>` + "\n")
	return []byte(b.String())
}

// xmpCoord renders decimal degrees as XMP's "degrees,decimal-minutes" with the
// hemisphere letter — the form exif:GPSLatitude and exif:GPSLongitude take.
func xmpCoord(v float64, pos, neg string) string {
	h := pos
	if v < 0 {
		h, v = neg, -v
	}
	d := math.Floor(v)
	minutes := (v - d) * 60
	return fmt.Sprintf("%d,%.6f%s", int(d), minutes, h)
}

// formatXMPTime is the ISO 8601 rendering XMP asks for, keeping the fraction
// only when there is one and the zone only when it is a recorded fact — XMP's
// date form makes the time zone optional for exactly this case.
func formatXMPTime(t CaptureTime) string {
	layout := "2006-01-02T15:04:05"
	if t.Value.Nanosecond() != 0 {
		layout += ".999999999"
	}
	if t.HasOffset {
		layout += "-07:00"
	}
	return t.Value.Format(layout)
}

// escapeXML escapes what a user typed into a form. A copyright line with an
// ampersand in it must not be able to break the sidecar.
func escapeXML(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			// Control characters are not legal in XML 1.0 at all, and a
			// form field is exactly where one arrives by accident.
			if r == '\n' || r == '\t' || r >= 0x20 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
