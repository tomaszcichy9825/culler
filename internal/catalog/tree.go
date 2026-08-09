package catalog

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Node is one folder in the library tree: a root at the top level, or a
// directory found under one.
//
// The counts are of everything at or under the node, because that is what the
// user is asking when they look at a folder they have not opened. Direct is
// what is filed in the folder itself, which is the part the count badge would
// otherwise lie about for a folder that only holds other folders.
type Node struct {
	Path      string
	Name      string
	Frames    int
	Direct    int
	RawBytes  int64
	JpegBytes int64
	// Dirs is every catalogued directory at or under the node, itself
	// included when it holds frames of its own. It is bounded by the number of
	// folders rather than the number of frames, which is what lets a caller
	// overlay live decisions onto a node without walking every frame under it.
	Dirs []string
	// HasDirs says whether Children would return anything, so the tree can draw
	// a twisty without asking.
	HasDirs bool
}

// Bytes is everything under the node.
func (n Node) Bytes() int64 { return n.RawBytes + n.JpegBytes }

// RootNodes is the top level of the tree: every registered root, oldest first,
// with what is catalogued under it.
func (s *Store) RootNodes() ([]Node, error) {
	paths, err := rootPaths(s.db)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(paths))
	for _, path := range paths {
		totals, err := s.dirTotals(path)
		if err != nil {
			return nil, err
		}
		node := Node{Path: path, Name: filepath.Base(path)}
		for _, t := range totals {
			node.Frames += t.frames
			node.RawBytes += t.rawBytes
			node.JpegBytes += t.jpegBytes
			node.Dirs = append(node.Dirs, t.dir)
			if t.dir == path {
				node.Direct = t.frames
			} else {
				node.HasDirs = true
			}
		}
		sort.Strings(node.Dirs)
		out = append(out, node)
	}
	return out, nil
}

// Children is the immediate subdirectories of dir that have anything
// catalogued at or under them, in name order.
//
// A folder that holds no frames of its own is still returned when something
// under it does: it is the way down to that folder, and a tree that hid it
// would put the frames out of reach.
func (s *Store) Children(dir string) ([]Node, error) {
	parent, err := cleanRoot(dir)
	if err != nil {
		return nil, err
	}
	totals, err := s.dirTotals(parent)
	if err != nil {
		return nil, err
	}

	// childPrefix, not parent + separator: a separator-terminated parent — the
	// filesystem root, or a drive root like C:\ — would otherwise build a
	// doubled prefix no catalogued dir carries, and this method has to agree
	// with under and underRoot on what counts as inside.
	prefix := childPrefix(parent)
	byName := map[string]*Node{}
	var order []string
	for _, t := range totals {
		if !strings.HasPrefix(t.dir, prefix) {
			continue // the parent's own frames belong to the parent
		}
		rest := t.dir[len(prefix):]
		name, deeper, _ := strings.Cut(rest, string(filepath.Separator))
		if name == "" {
			continue
		}
		child, ok := byName[name]
		if !ok {
			child = &Node{Path: filepath.Join(parent, name), Name: name}
			byName[name] = child
			order = append(order, name)
		}
		child.Frames += t.frames
		child.RawBytes += t.rawBytes
		child.JpegBytes += t.jpegBytes
		child.Dirs = append(child.Dirs, t.dir)
		if deeper == "" {
			child.Direct = t.frames
		} else {
			child.HasDirs = true
		}
	}

	sort.Strings(order)
	out := make([]Node, 0, len(order))
	for _, name := range order {
		child := byName[name]
		sort.Strings(child.Dirs)
		out = append(out, *child)
	}
	return out, nil
}

// Folder is one directory the catalogue covers: a registered root, a folder
// frames are filed in, or a level between the two.
//
// It is deliberately flatter than Node. A Node is a row of the tree, drawn with
// its byte totals and its twisty; a Folder is a place a photograph could be
// filed, which is a list a palette fuzzy-matches against and nothing else.
type Folder struct {
	Path string
	Name string
	// Frames is everything at or under the folder, and Direct is what is filed
	// in the folder itself. A level that only leads somewhere else has a Direct
	// of zero and is still a perfectly good destination.
	Frames int
	Direct int
}

// DefaultFolderLimit is how many folders Dirs offers a caller that names no
// limit of its own. A palette shows a handful and fuzzy-matches the rest, so
// the cap is about what a list can usefully be rather than what SQLite can
// return.
const DefaultFolderLimit = 500

// Dirs is the folders the catalogue knows about, busiest first, capped at
// limit — zero or less taking DefaultFolderLimit.
//
// Every level between a root and a folder holding frames is named, because a
// year folder nothing is filed directly in is still somewhere to file a
// photograph, and a list that only held leaf folders would not offer it.
//
// The one query behind this groups by directory, so what it reads is bounded by
// the shape of the tree rather than by how many frames are in it — the same
// property dirTotals leans on. The rollup and the cap are done here rather than
// in SQL because the ancestors do not exist as rows to be counted.
func (s *Store) Dirs(limit int) ([]Folder, error) {
	if limit <= 0 {
		limit = DefaultFolderLimit
	}
	roots, err := rootPaths(s.db)
	if err != nil {
		return nil, err
	}
	totals, err := s.allDirTotals()
	if err != nil {
		return nil, err
	}

	byPath := map[string]*Folder{}
	folder := func(path string) *Folder {
		f, ok := byPath[path]
		if !ok {
			f = &Folder{Path: path, Name: filepath.Base(path)}
			byPath[path] = f
		}
		return f
	}
	for _, t := range totals {
		folder(t.dir).Direct += t.frames
		// The frames count towards the folder itself and towards every level
		// between it and the root it lives under. A directory no root covers —
		// a root removed since the last pass, say — still names itself, so a
		// folder the user has been filing into does not vanish from the list
		// before the next index pass notices.
		for _, dir := range ancestry(t.dir, roots) {
			folder(dir).Frames += t.frames
		}
	}

	out := make([]Folder, 0, len(byPath))
	for _, f := range byPath {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Frames != out[j].Frames {
			return out[i].Frames > out[j].Frames
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ancestry is dir and every folder between it and the root it sits under, the
// root included. A dir no root covers is only itself.
func ancestry(dir string, roots []string) []string {
	out := []string{dir}
	root := ""
	for _, r := range roots {
		// The longest root wins, so a root nested inside another does not walk
		// the ancestry past the folder the user actually registered.
		if under(dir, r) && len(r) > len(root) {
			root = r
		}
	}
	if root == "" || root == dir {
		return out
	}
	for up := filepath.Dir(dir); under(up, root); up = filepath.Dir(up) {
		out = append(out, up)
		if up == root || up == filepath.Dir(up) {
			break
		}
	}
	return out
}

// allDirTotals is dirTotals over the whole catalogue rather than one subtree.
func (s *Store) allDirTotals() ([]dirTotal, error) {
	rows, err := s.db.Query(
		`SELECT dir, COUNT(*), COALESCE(SUM(raw_bytes),0), COALESCE(SUM(jpeg_bytes),0)
		 FROM frames GROUP BY dir`)
	if err != nil {
		return nil, fmt.Errorf("catalog: total every folder: %w", err)
	}
	defer rows.Close()

	var out []dirTotal
	for rows.Next() {
		var t dirTotal
		if err := rows.Scan(&t.dir, &t.frames, &t.rawBytes, &t.jpegBytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// dirTotal is one catalogued directory and what is filed directly in it.
type dirTotal struct {
	dir       string
	frames    int
	rawBytes  int64
	jpegBytes int64
}

// dirTotals groups the frames at or under dir by the directory they are filed
// in. One row per folder, so the result is bounded by the shape of the tree
// rather than by how many frames are in it.
func (s *Store) dirTotals(dir string) ([]dirTotal, error) {
	where, args := underRoot(dir)
	rows, err := s.db.Query(
		`SELECT dir, COUNT(*), COALESCE(SUM(raw_bytes),0), COALESCE(SUM(jpeg_bytes),0)
		 FROM frames WHERE `+where+` GROUP BY dir`, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: total folders under %s: %w", dir, err)
	}
	defer rows.Close()

	var out []dirTotal
	for rows.Next() {
		var t dirTotal
		if err := rows.Scan(&t.dir, &t.frames, &t.rawBytes, &t.jpegBytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// KeysUnder is the identity of every frame at or under dir.
//
// It is how a caller asks the decision store what is still undecided in a
// folder without the catalogue having to know what a decision is. The cost is
// one row per frame, so it belongs behind something the user did — expanding a
// node — rather than in a redraw.
func (s *Store) KeysUnder(dir string) ([]FrameKey, error) {
	clean, err := cleanRoot(dir)
	if err != nil {
		return nil, err
	}
	where, args := underRoot(clean)
	rows, err := s.db.Query(`SELECT hash, dir, stem FROM frames WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: read frames under %s: %w", clean, err)
	}
	defer rows.Close()

	out := []FrameKey{}
	for rows.Next() {
		var k FrameKey
		if err := rows.Scan(&k.Hash, &k.Dir, &k.Stem); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
