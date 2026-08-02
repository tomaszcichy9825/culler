// Package hash computes the cheap identity hash that keys a frame's decision
// and its cached thumbnail. It reads the head of the file rather than all of
// it: a 50MB RAW would otherwise cost a full read per frame on a card reader,
// and the first 64KB plus the size already separates real photographs.
package hash

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// prefixBytes is how much of the file head goes into the hash.
const prefixBytes = 64 << 10

// Content returns the identity hash of the file at path as a hex string: a
// sha256 over the first 64KB of the file plus its size. It survives a rename
// because nothing about the path is hashed, and it changes when the file is
// edited because both the head and the length are covered.
func Content(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("hash: %s is a directory", path)
	}

	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, prefixBytes)); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	// Length goes in after the head so that two files sharing a 64KB prefix
	// but differing in size get different hashes.
	if err := binary.Write(h, binary.LittleEndian, info.Size()); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
