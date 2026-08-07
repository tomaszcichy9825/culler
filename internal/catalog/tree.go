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

	prefix := parent + string(filepath.Separator)
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
