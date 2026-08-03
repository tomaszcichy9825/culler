package decide

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MaxSlots is how many destinations the keyboard can reach by digit. Nine,
// because that is how many digit keys are left once 0 is the one that clears.
const MaxSlots = 9

// Destination is one place frames get routed to, and what the palette knows
// about it.
type Destination struct {
	Path       string
	Label      string
	LastUsedAt time.Time
	UseCount   int
	Pinned     bool
	// Slot is the digit the user bound this destination to by hand, 0 when
	// they have not bound one. A bound slot is never taken away by a newer
	// destination; that is the whole point of binding it.
	Slot int
	// Digit is the key that reaches this destination right now: its own slot
	// when it has one, otherwise the first digit recency has left free. Zero
	// means no digit reaches it — it is past the ninth row.
	Digit int
}

// UseDestination records that frames have just been routed to path: it is
// remembered if it is new, and moves to the top of the recent list either way.
// A non-empty label renames it; an empty one leaves whatever name it had.
func (s *Store) UseDestination(path, label string) error {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return fmt.Errorf("decide: a destination needs a path")
	}
	_, err := s.db.Exec(`
INSERT INTO destinations (path, label, last_used_at, use_count, pinned, slot)
VALUES (?, ?, ?, 1, 0, NULL)
ON CONFLICT(path) DO UPDATE SET
	label = CASE WHEN excluded.label <> '' THEN excluded.label ELSE destinations.label END,
	last_used_at = excluded.last_used_at,
	use_count = destinations.use_count + 1
`, clean, strings.TrimSpace(label), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("decide: remember destination %s: %w", clean, err)
	}
	return nil
}

// ForgetDestination drops a destination from the palette. Nothing on disk
// moves and no frame already routed there is disturbed; the app simply stops
// offering it.
func (s *Store) ForgetDestination(path string) error {
	_, err := s.db.Exec(`DELETE FROM destinations WHERE path = ?`, path)
	if err != nil {
		return fmt.Errorf("decide: forget destination %s: %w", path, err)
	}
	return nil
}

// PinDestination holds a destination at the top of the palette, above the
// recent ones, until it is unpinned.
func (s *Store) PinDestination(path string, pinned bool) error {
	res, err := s.db.Exec(`UPDATE destinations SET pinned = ? WHERE path = ?`, boolToInt(pinned), path)
	if err != nil {
		return fmt.Errorf("decide: pin destination %s: %w", path, err)
	}
	return requireRow(res, path)
}

// BindSlot nails a destination to a digit, or releases it with slot 0. A digit
// names one place, so binding one takes it off whoever held it.
func (s *Store) BindSlot(path string, slot int) error {
	if slot < 0 || slot > MaxSlots {
		return fmt.Errorf("decide: slot %d is off the 1-%d keyboard", slot, MaxSlots)
	}
	return s.inTx(func(tx *sql.Tx) error {
		if slot > 0 {
			if _, err := tx.Exec(`UPDATE destinations SET slot = NULL WHERE slot = ?`, slot); err != nil {
				return fmt.Errorf("decide: release slot %d: %w", slot, err)
			}
		}
		var value any
		if slot > 0 {
			value = slot
		}
		res, err := tx.Exec(`UPDATE destinations SET slot = ? WHERE path = ?`, value, path)
		if err != nil {
			return fmt.Errorf("decide: bind %s to slot %d: %w", path, slot, err)
		}
		return requireRow(res, path)
	})
}

// Destinations lists every remembered destination in palette order: pinned
// first, then most recently used. The digit each one answers to is worked out
// here rather than stored, so it follows recency without a write on every use.
func (s *Store) Destinations() ([]Destination, error) {
	rows, err := s.db.Query(`
SELECT path, label, last_used_at, use_count, pinned, COALESCE(slot, 0)
FROM destinations
ORDER BY pinned DESC, last_used_at DESC, path
`)
	if err != nil {
		return nil, fmt.Errorf("decide: list destinations: %w", err)
	}
	defer rows.Close()

	var out []Destination
	for rows.Next() {
		var d Destination
		var used int64
		var pinned int
		if err := rows.Scan(&d.Path, &d.Label, &used, &d.UseCount, &pinned, &d.Slot); err != nil {
			return nil, err
		}
		d.Pinned = pinned != 0
		if used > 0 {
			d.LastUsedAt = time.Unix(used, 0)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	assignDigits(out)
	return out, nil
}

// DestinationForDigit resolves what pressing a digit means right now. The
// second result is false when nothing has claimed that key.
func (s *Store) DestinationForDigit(digit int) (Destination, bool, error) {
	if digit < 1 || digit > MaxSlots {
		return Destination{}, false, nil
	}
	list, err := s.Destinations()
	if err != nil {
		return Destination{}, false, err
	}
	for _, d := range list {
		if d.Digit == digit {
			return d, true, nil
		}
	}
	return Destination{}, false, nil
}

// assignDigits works out which digit reaches each destination, in place.
// Bound slots are honoured first so a newer destination cannot shuffle one out
// from under the user's fingers; whatever digits are left go to the rest in
// palette order, which is what makes the nine most recent reachable without
// anybody having bound anything.
//
// Two destinations bound to the same digit cannot both have it — the store
// stops that happening — but a hand-edited database could, so the first in
// order keeps it and the other falls back to a free one.
func assignDigits(list []Destination) {
	taken := [MaxSlots + 1]bool{}
	for i := range list {
		slot := list[i].Slot
		if slot >= 1 && slot <= MaxSlots && !taken[slot] {
			taken[slot] = true
			list[i].Digit = slot
		}
	}
	next := 1
	for i := range list {
		if list[i].Digit != 0 {
			continue
		}
		for next <= MaxSlots && taken[next] {
			next++
		}
		if next > MaxSlots {
			return
		}
		taken[next] = true
		list[i].Digit = next
	}
}

// requireRow turns "no such destination" into an error rather than a silent
// no-op, so a palette acting on a row that has been forgotten says so.
func requireRow(res sql.Result, path string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("decide: no destination at %s", path)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
