package catalog

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultSessionGap is how long a break in shooting has to be before the
// frames after it count as a different session. Four hours puts a morning and
// an evening on the same day in separate sessions and keeps a long afternoon
// in one.
const DefaultSessionGap = 4 * time.Hour

// Facets narrow a search. Every zero field is "no opinion", so the zero value
// searches everything.
type Facets struct {
	Kind      string    // paired | jpeg-only | raw-only
	Verdict   string    // keep | cut | undecided
	MinRating int       // 1–5
	Root      string    // only frames under this folder
	From      time.Time // inclusive
	To        time.Time // exclusive
}

// Page is one window onto a result. A Limit of zero returns everything, which
// is what the facet counts and the session view want.
type Page struct {
	Limit  int
	Offset int
}

// Results is a page of frames plus the size of the whole result, so the title
// bar can say "184 results" while showing forty of them.
type Results struct {
	Frames []Frame
	Total  int
	Offset int
}

// Search returns the frames matching query and facets, newest first.
//
// The query is free text matched against the stem and the folder, and may
// carry `key:value` tokens. A facet passed explicitly wins over the same facet
// written in the query: the chips are the deliberate control, and a token left
// behind in the field should not quietly override the chip the user just
// clicked.
func (s *Store) Search(query string, f Facets, p Page) (Results, error) {
	text, fromQuery := ParseQuery(query)
	f = mergeFacets(f, fromQuery)

	where, args, err := searchWhere(text, f)
	if err != nil {
		return Results{}, err
	}

	var out Results
	out.Offset = p.Offset
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM frames WHERE `+where, args...).Scan(&out.Total); err != nil {
		return Results{}, fmt.Errorf("catalog: count search: %w", err)
	}

	q := `SELECT hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes, rating, verdict
	      FROM frames WHERE ` + where + ` ORDER BY shot DESC, stem ASC`
	page := args
	if p.Limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		page = append(append([]any{}, args...), p.Limit, max(0, p.Offset))
	} else if p.Offset > 0 {
		q += ` LIMIT -1 OFFSET ?`
		page = append(append([]any{}, args...), p.Offset)
	}

	frames, err := s.frames(q, page...)
	if err != nil {
		return Results{}, err
	}
	out.Frames = frames
	return out, nil
}

// frames runs a select over the frames table and reads the rows.
func (s *Store) frames(query string, args ...any) ([]Frame, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: read frames: %w", err)
	}
	defer rows.Close()

	out := []Frame{}
	for rows.Next() {
		var f Frame
		var shot int64
		if err := rows.Scan(&f.Hash, &f.Dir, &f.Stem, &f.Kind, &shot,
			&f.RawPath, &f.JpegPath, &f.RawBytes, &f.JpegBytes, &f.Rating, &f.Verdict); err != nil {
			return nil, err
		}
		f.Shot = time.Unix(shot, 0).UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

// searchWhere turns free text and facets into a WHERE clause.
func searchWhere(text string, f Facets) (string, []any, error) {
	clauses := []string{"1 = 1"}
	var args []any

	if text != "" {
		// Escaped so that a folder called "100%" is a search for that folder
		// and not for everything.
		pattern := "%" + escapeLike(strings.ToLower(text)) + "%"
		clauses = append(clauses, `(lower(stem) LIKE ? ESCAPE '\' OR lower(dir) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if f.Kind != "" {
		if !validKind(f.Kind) {
			return "", nil, fmt.Errorf("catalog: unknown kind %q", f.Kind)
		}
		clauses = append(clauses, `kind = ?`)
		args = append(args, f.Kind)
	}
	switch f.Verdict {
	case "":
	case VerdictKeep, VerdictCut:
		clauses = append(clauses, `verdict = ?`)
		args = append(args, f.Verdict)
	case VerdictNone:
		clauses = append(clauses, `verdict = ''`)
	default:
		return "", nil, fmt.Errorf("catalog: unknown verdict %q", f.Verdict)
	}
	if f.MinRating > 0 {
		clauses = append(clauses, `rating >= ?`)
		args = append(args, f.MinRating)
	}
	if f.Root != "" {
		root, err := cleanRoot(f.Root)
		if err != nil {
			return "", nil, err
		}
		clause, extra := underRoot(root)
		clauses = append(clauses, clause)
		args = append(args, extra...)
	}
	if !f.From.IsZero() {
		clauses = append(clauses, `shot >= ?`)
		args = append(args, f.From.Unix())
	}
	if !f.To.IsZero() {
		clauses = append(clauses, `shot < ?`)
		args = append(args, f.To.Unix())
	}
	return strings.Join(clauses, " AND "), args, nil
}

func validKind(kind string) bool {
	switch kind {
	case "paired", "jpeg-only", "raw-only":
		return true
	}
	return false
}

// escapeLike neutralises the three characters LIKE reads as syntax.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ParseQuery splits a query field into the free text and the facets its
// `key:value` tokens describe. A token whose key or value the catalogue does
// not know is left in the text, so the user sees it matching nothing rather
// than silently doing nothing.
func ParseQuery(query string) (string, Facets) {
	var f Facets
	var text []string
	for _, token := range strings.Fields(query) {
		key, value, found := strings.Cut(token, ":")
		if !found || value == "" {
			text = append(text, token)
			continue
		}
		switch strings.ToLower(key) {
		case "kind":
			if validKind(strings.ToLower(value)) {
				f.Kind = strings.ToLower(value)
				continue
			}
		case "verdict":
			switch v := strings.ToLower(value); v {
			case VerdictKeep, VerdictCut, VerdictNone:
				f.Verdict = v
				continue
			}
		case "rating":
			if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= 5 {
				f.MinRating = n
				continue
			}
		case "root":
			if filepath.IsAbs(value) {
				f.Root = filepath.Clean(value)
				continue
			}
		}
		text = append(text, token)
	}
	return strings.Join(text, " "), f
}

// mergeFacets fills the gaps in explicit with what the query said. Explicit
// wins field by field rather than wholesale, so a chip and a token can narrow
// a search together.
func mergeFacets(explicit, fromQuery Facets) Facets {
	if explicit.Kind == "" {
		explicit.Kind = fromQuery.Kind
	}
	if explicit.Verdict == "" {
		explicit.Verdict = fromQuery.Verdict
	}
	if explicit.MinRating == 0 {
		explicit.MinRating = fromQuery.MinRating
	}
	if explicit.Root == "" {
		explicit.Root = fromQuery.Root
	}
	return explicit
}

// Counts is how many frames sit behind each facet value, which is what the
// meters in the facet lists are drawn from.
type Counts struct {
	// Total is the size of the current result, the same number Search reports.
	Total int
	// Kinds and Verdicts are keyed by facet value; Verdicts uses VerdictNone
	// for the frames nobody has judged.
	Kinds    map[string]int
	Verdicts map[string]int
	// Ratings is keyed by star, counted the way the facet filters: the entry
	// for three is every frame of three stars or more.
	Ratings map[int]int
}

// Counts totals each facet's values under the current query.
//
// A facet is counted with its own selection cleared, so choosing "raw only"
// leaves the other kinds visible with the counts they would have if it were
// unselected. A list that narrowed itself would leave the user nothing to
// click their way back through.
func (s *Store) Counts(query string, f Facets) (Counts, error) {
	text, fromQuery := ParseQuery(query)
	f = mergeFacets(f, fromQuery)

	out := Counts{
		Kinds:    map[string]int{},
		Verdicts: map[string]int{},
		Ratings:  map[int]int{},
	}

	where, args, err := searchWhere(text, f)
	if err != nil {
		return Counts{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM frames WHERE `+where, args...).Scan(&out.Total); err != nil {
		return Counts{}, fmt.Errorf("catalog: count facets: %w", err)
	}

	withoutKind := f
	withoutKind.Kind = ""
	if err := s.countBy(&out, text, withoutKind, "kind"); err != nil {
		return Counts{}, err
	}
	withoutVerdict := f
	withoutVerdict.Verdict = ""
	if err := s.countBy(&out, text, withoutVerdict, "verdict"); err != nil {
		return Counts{}, err
	}
	withoutRating := f
	withoutRating.MinRating = 0
	if err := s.countBy(&out, text, withoutRating, "rating"); err != nil {
		return Counts{}, err
	}
	return out, nil
}

// countBy groups one column and files the results on out.
func (s *Store) countBy(out *Counts, text string, f Facets, column string) error {
	where, args, err := searchWhere(text, f)
	if err != nil {
		return err
	}
	rows, err := s.db.Query(
		`SELECT `+column+`, COUNT(*) FROM frames WHERE `+where+` GROUP BY `+column, args...)
	if err != nil {
		return fmt.Errorf("catalog: count by %s: %w", column, err)
	}
	defer rows.Close()

	for rows.Next() {
		var value string
		var n int
		if err := rows.Scan(&value, &n); err != nil {
			return err
		}
		switch column {
		case "kind":
			out.Kinds[value] = n
		case "verdict":
			if value == "" {
				value = VerdictNone
			}
			out.Verdicts[value] = n
		case "rating":
			stars, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			// Counted cumulatively, because the facet filters on "and up".
			for star := 1; star <= stars; star++ {
				out.Ratings[star] += n
			}
		}
	}
	return rows.Err()
}

// Session is a shoot: the frames whose shot times run together with no break
// longer than the gap. Sessions are derived on every query rather than stored,
// because the gap is the user's setting and reindexing to change it would be
// absurd.
type Session struct {
	// ID is stable for as long as the session's first frame is: the start
	// time in unix seconds, which is what the UI keys its rows on.
	ID        string
	Start     time.Time
	End       time.Time
	Frames    int
	Kept      int
	Cut       int
	Undecided int
	RawBytes  int64
	JpegBytes int64
	// Source is the folder most of the session's frames came from, named the
	// way the user would — the folder's own name, not its path.
	Source string
	Dir    string
	Dirs   int
}

// Span is how long the session ran.
func (s Session) Span() time.Duration { return s.End.Sub(s.Start) }

// Sessions returns every session in the catalogue, newest first. A gap of zero
// takes DefaultSessionGap.
func (s *Store) Sessions(gap time.Duration) ([]Session, error) {
	if gap <= 0 {
		gap = DefaultSessionGap
	}
	frames, err := s.frames(
		`SELECT hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes, rating, verdict
		 FROM frames ORDER BY shot ASC, stem ASC`)
	if err != nil {
		return nil, err
	}

	out := []Session{}
	var current *Session
	var dirs map[string]int
	var last time.Time

	flush := func() {
		if current == nil {
			return
		}
		current.Source, current.Dir, current.Dirs = dominantDir(dirs)
		out = append(out, *current)
		current = nil
	}

	for _, f := range frames {
		if current == nil || f.Shot.Sub(last) > gap {
			flush()
			current = &Session{ID: strconv.FormatInt(f.Shot.Unix(), 10), Start: f.Shot}
			dirs = map[string]int{}
		}
		current.End = f.Shot
		current.Frames++
		current.RawBytes += f.RawBytes
		current.JpegBytes += f.JpegBytes
		switch f.Verdict {
		case VerdictKeep:
			current.Kept++
		case VerdictCut:
			current.Cut++
		default:
			current.Undecided++
		}
		dirs[f.Dir]++
		last = f.Shot
	}
	flush()

	// Built oldest first because that is the order the clustering needs;
	// shown newest first because that is the order the user thinks in.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// dominantDir picks the folder that contributed most of a session's frames,
// ties broken by name so the answer does not move between runs.
func dominantDir(dirs map[string]int) (name, dir string, count int) {
	best := ""
	for d, n := range dirs {
		if best == "" || n > dirs[best] || (n == dirs[best] && d < best) {
			best = d
		}
	}
	if best == "" {
		return "", "", 0
	}
	return filepath.Base(best), best, len(dirs)
}

// RootStorage is what one root is holding.
type RootStorage struct {
	Root      string
	Volume    string
	Frames    int
	RawBytes  int64
	JpegBytes int64
}

// Bytes is the root's total footprint.
func (r RootStorage) Bytes() int64 { return r.RawBytes + r.JpegBytes }

// VolumeStorage rolls the roots on one volume together, which is the unit the
// storage view draws a card for.
type VolumeStorage struct {
	Volume    string
	Frames    int
	RawBytes  int64
	JpegBytes int64
	Roots     []string
}

// Bytes is the volume's total footprint, as far as the catalogue knows.
func (v VolumeStorage) Bytes() int64 { return v.RawBytes + v.JpegBytes }

// Storage is the whole catalogue's footprint, per root and per volume.
type Storage struct {
	Frames    int
	RawBytes  int64
	JpegBytes int64
	Roots     []RootStorage
	Volumes   []VolumeStorage
}

// StorageSummary totals the catalogue by root and rolls those up by volume.
// A registered root that has never been indexed appears with nothing in it,
// because an empty row is how the user finds out it was never indexed.
//
// The totals are of what the catalogue holds, not of what the disk holds:
// nothing here stats the filesystem, so free space is the caller's to find.
func (s *Store) StorageSummary() (Storage, error) {
	roots, err := s.Roots()
	if err != nil {
		return Storage{}, err
	}

	out := Storage{Roots: make([]RootStorage, 0, len(roots))}
	volumes := map[string]*VolumeStorage{}
	var order []string

	for _, r := range roots {
		rs := RootStorage{
			Root:      r.Path,
			Volume:    r.Volume,
			Frames:    r.Frames,
			RawBytes:  r.RawBytes,
			JpegBytes: r.JpegBytes,
		}
		out.Roots = append(out.Roots, rs)

		v, ok := volumes[r.Volume]
		if !ok {
			v = &VolumeStorage{Volume: r.Volume}
			volumes[r.Volume] = v
			order = append(order, r.Volume)
		}
		v.Frames += rs.Frames
		v.RawBytes += rs.RawBytes
		v.JpegBytes += rs.JpegBytes
		v.Roots = append(v.Roots, rs.Root)
	}

	// The catalogue's own total rather than the sum of the roots: a frame
	// covered by two nested roots is one frame, and counting it twice would
	// make the storage view disagree with the search that found it.
	err = s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(raw_bytes),0), COALESCE(SUM(jpeg_bytes),0) FROM frames`,
	).Scan(&out.Frames, &out.RawBytes, &out.JpegBytes)
	if err != nil {
		return Storage{}, fmt.Errorf("catalog: total catalogue: %w", err)
	}

	// Biggest volume first, since that is the one the user came to reclaim.
	sort.SliceStable(order, func(i, j int) bool {
		a, b := volumes[order[i]], volumes[order[j]]
		if a.Bytes() != b.Bytes() {
			return a.Bytes() > b.Bytes()
		}
		return a.Volume < b.Volume
	})
	out.Volumes = make([]VolumeStorage, 0, len(order))
	for _, name := range order {
		out.Volumes = append(out.Volumes, *volumes[name])
	}
	return out, nil
}
