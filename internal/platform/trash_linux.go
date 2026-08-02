//go:build linux

package platform

// SystemTrasher returns the platform's user-recoverable trash: the home trash
// directory defined by the freedesktop.org trash specification, which every
// mainstream desktop shows as "Rubbish Bin".
//
// Files on other volumes are trashed into the home trash too, which the spec
// permits and which turns those deletions into a copy across the device
// boundary rather than a rename. Per-volume .Trash-$uid directories would keep
// them on the card, and the card is never written to.
func SystemTrasher() (Trasher, error) {
	root, err := xdgTrashRoot()
	if err != nil {
		return nil, err
	}
	return xdgTrash(root), nil
}
