package catalog

import (
	"path/filepath"
	"testing"
)

// shootTree is a root shaped the way a photographer's archive is: a year, two
// months under it, and a card folder one deeper, with frames at more than one
// level so the aggregate counts have something to add up.
func shootTree(t *testing.T) (*Store, string) {
	t.Helper()
	s := openStore(t)
	root := t.TempDir()
	may := filepath.Join(root, "2026-05")
	june := filepath.Join(root, "2026-06")
	card := filepath.Join(may, "100_FUJI")
	mkdir(t, may)
	mkdir(t, june)
	mkdir(t, card)

	writeFrame(t, root, "LOOSE001", 100, 0, shotAt(8, 0))
	writeFrame(t, may, "DSCF0001", 100, 0, shotAt(9, 0))
	writeFrame(t, may, "DSCF0002", 100, 200, shotAt(9, 1))
	writeFrame(t, card, "DSCF0100", 100, 0, shotAt(10, 0))
	writeFrame(t, june, "DSCF0200", 100, 0, shotAt(11, 0))

	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return s, root
}

func nodeByName(t *testing.T, nodes []Node, name string) Node {
	t.Helper()
	for _, n := range nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("no node called %q in %+v", name, nodes)
	return Node{}
}

func TestRootNodesCarryTheirTopLevelCounts(t *testing.T) {
	s, root := shootTree(t)

	nodes, err := s.RootNodes()
	if err != nil {
		t.Fatalf("RootNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("%d root nodes, want 1: %+v", len(nodes), nodes)
	}
	n := nodes[0]
	if n.Path != root {
		t.Errorf("root node path = %s, want %s", n.Path, root)
	}
	if n.Name != filepath.Base(root) {
		t.Errorf("root node name = %s, want the folder's own name %s", n.Name, filepath.Base(root))
	}
	if n.Frames != 5 {
		t.Errorf("root node holds %d frames, want all 5 under it", n.Frames)
	}
	if n.Direct != 1 {
		t.Errorf("root node has %d frames of its own, want the 1 loose frame", n.Direct)
	}
	if !n.HasDirs {
		t.Error("root node says it has no subdirectories")
	}
	if n.Bytes() == 0 {
		t.Error("root node reports no bytes")
	}
}

// One folder the user registered is one row at the top of the tree. The
// folders under it are reached by opening it, never by appearing beside it.
func TestOneRegisteredRootIsExactlyOneTopLevelNode(t *testing.T) {
	s, root := shootTree(t)

	nodes, err := s.RootNodes()
	if err != nil {
		t.Fatalf("RootNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("%d top-level nodes for one registered root: %+v", len(nodes), nodes)
	}
	if nodes[0].Path != root {
		t.Errorf("the top-level node is %s, want the registered root %s", nodes[0].Path, root)
	}

	// The frame-bearing folders under it are children, and nothing else.
	children, err := s.Children(root)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	for _, child := range children {
		for _, top := range nodes {
			if child.Path == top.Path {
				t.Errorf("%s is drawn both under the root and beside it", child.Path)
			}
		}
	}
	if len(children) == 0 {
		t.Error("the root's subfolders are not reachable by expanding it")
	}
}

func TestChildrenAreTheImmediateSubdirectories(t *testing.T) {
	s, root := shootTree(t)

	children, err := s.Children(root)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("%d children of the root, want the 2 month folders: %+v", len(children), children)
	}
	// Sorted by name, so the tree does not reshuffle between expansions.
	if children[0].Name != "2026-05" || children[1].Name != "2026-06" {
		t.Errorf("children = %s, %s, want them in name order", children[0].Name, children[1].Name)
	}

	may := nodeByName(t, children, "2026-05")
	if may.Frames != 3 {
		t.Errorf("2026-05 holds %d frames, want the 2 of its own plus the card's 1", may.Frames)
	}
	if may.Direct != 2 {
		t.Errorf("2026-05 has %d frames of its own, want 2", may.Direct)
	}
	if !may.HasDirs {
		t.Error("2026-05 says it has no subdirectories, but the card folder is under it")
	}
	if may.JpegBytes == 0 {
		t.Error("2026-05 reports no JPEG bytes, but one of its frames is paired")
	}

	june := nodeByName(t, children, "2026-06")
	if june.Frames != 1 || june.Direct != 1 {
		t.Errorf("2026-06 = %d frames (%d of its own), want 1 and 1", june.Frames, june.Direct)
	}
	if june.HasDirs {
		t.Error("2026-06 says it has subdirectories, but nothing is catalogued under it")
	}
}

func TestChildrenSkipsTheLevelsNothingIsFiledAt(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	deep := filepath.Join(root, "2026", "05", "01")
	mkdir(t, deep)
	writeFrame(t, deep, "DSCF0001", 100, 0, shotAt(9, 0))
	if _, err := s.Index(root, IndexOptions{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// The intermediate folders hold no frames of their own, but they are still
	// the way down to the one that does, so the tree has to offer them.
	children, err := s.Children(root)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 1 || children[0].Name != "2026" {
		t.Fatalf("children = %+v, want the 2026 folder alone", children)
	}
	if children[0].Frames != 1 {
		t.Errorf("2026 holds %d frames, want the 1 buried under it", children[0].Frames)
	}
	if children[0].Direct != 0 {
		t.Errorf("2026 claims %d frames of its own, want none", children[0].Direct)
	}
	if !children[0].HasDirs {
		t.Error("2026 says it has no subdirectories")
	}
}

func TestChildrenOfALeafIsEmpty(t *testing.T) {
	s, root := shootTree(t)

	children, err := s.Children(filepath.Join(root, "2026-06"))
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("a leaf reports %d children: %+v", len(children), children)
	}
}

func TestChildrenOfSomethingUncataloguedIsEmpty(t *testing.T) {
	s, _ := shootTree(t)

	children, err := s.Children(t.TempDir())
	if err != nil {
		t.Fatalf("Children of an uncatalogued folder: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("an uncatalogued folder reports %d children", len(children))
	}
}

func TestChildrenRejectsARelativePath(t *testing.T) {
	s, _ := shootTree(t)
	if _, err := s.Children("2026-05"); err == nil {
		t.Error("a relative path was accepted")
	}
}

func TestKeysUnderCoversTheWholeSubtree(t *testing.T) {
	s, root := shootTree(t)

	under, err := s.KeysUnder(filepath.Join(root, "2026-05"))
	if err != nil {
		t.Fatalf("KeysUnder: %v", err)
	}
	if len(under) != 3 {
		t.Errorf("2026-05 covers %d keys, want its 2 plus the card's 1", len(under))
	}
	seen := map[FrameKey]bool{}
	for _, k := range under {
		if k.Hash == "" || k.Dir == "" || k.Stem == "" {
			t.Errorf("KeysUnder returned a partial key: %+v", k)
		}
		if seen[k] {
			t.Errorf("KeysUnder returned %+v twice", k)
		}
		seen[k] = true
	}

	all, err := s.KeysUnder(root)
	if err != nil {
		t.Fatalf("KeysUnder: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("the root covers %d keys, want 5", len(all))
	}
}
