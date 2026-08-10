package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
)

// VerdictItem is one frame's verdict, as the frontend sends it.
type VerdictItem struct {
	Hash    string `json:"hash"`
	Dir     string `json:"dir"`
	Stem    string `json:"stem"`
	Verdict string `json:"verdict"` // "" | keep | cut
	Mask    string `json:"mask"`    // rj | r | j
}

// RatingItem is one frame's rating, as the frontend sends it.
type RatingItem struct {
	Hash   string `json:"hash"`
	Dir    string `json:"dir"`
	Stem   string `json:"stem"`
	Rating int    `json:"rating"` // 0 clears
}

// DestinationItem is one frame's destination, as the frontend sends it. An
// empty destination clears the routing and leaves the verdict alone.
type DestinationItem struct {
	Hash        string `json:"hash"`
	Dir         string `json:"dir"`
	Stem        string `json:"stem"`
	Destination string `json:"destination"`
	// Verb is how the frame travels: "move" or "copy", as the key that routed
	// it said. Empty leaves it to the configured default.
	Verb string `json:"verb"`
}

// DestinationDTO is one remembered destination, as the move palette shows it.
type DestinationDTO struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	// LastUsedAt is RFC3339, empty for a destination that has never been used.
	LastUsedAt string `json:"lastUsedAt"`
	UseCount   int    `json:"useCount"`
	Pinned     bool   `json:"pinned"`
	// Slot is the digit the user bound by hand, 0 when they have not.
	Slot int `json:"slot"`
	// Digit is the key that reaches this destination right now, 0 when none
	// does. This is the one the palette shows and the one a digit press means.
	Digit int `json:"digit"`
}

// DecisionItem is one frame's decision in the pre-verdict vocabulary. It stays
// until the grid is restyled onto verdicts and masks; see legacyDecisions.
type DecisionItem struct {
	Hash     string `json:"hash"`
	Dir      string `json:"dir"`
	Stem     string `json:"stem"`
	Decision string `json:"decision"`
}

// DecisionService records what the user decided about a frame. Decisions are
// cheap and reversible: they change nothing on disk until an apply.
type DecisionService struct {
	app *App
}

// NewDecisionService binds the service to the shared state.
func NewDecisionService(a *App) *DecisionService {
	return &DecisionService{app: a}
}

// SetVerdict records one verdict and the mask it applies to. An empty verdict
// clears it, leaving any rating on the frame alone.
func (s *DecisionService) SetVerdict(hash, dir, stem, verdict, mask string) error {
	item, err := toVerdictItem(VerdictItem{Hash: hash, Dir: dir, Stem: stem, Verdict: verdict, Mask: mask})
	if err != nil {
		return err
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetVerdict(item.Hash, item.Dir, item.Stem, item.Verdict, item.Mask)
}

// SetVerdictBatch records many verdicts in one transaction. The grid marks
// frames far faster than it should touch the disk, so it collects them and
// flushes through here. Either the whole batch lands or none of it does.
func (s *DecisionService) SetVerdictBatch(items []VerdictItem) error {
	converted := make([]decide.VerdictItem, 0, len(items))
	for _, it := range items {
		item, err := toVerdictItem(it)
		if err != nil {
			return err
		}
		converted = append(converted, item)
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetVerdictBatch(converted)
}

// SetRating records a 1-5 star rating, or 0 to clear it. Ratings are stored
// independently of verdicts: rating a frame nobody has judged yet is normal.
func (s *DecisionService) SetRating(hash, dir, stem string, rating int) error {
	item, err := toRatingItem(RatingItem{Hash: hash, Dir: dir, Stem: stem, Rating: rating})
	if err != nil {
		return err
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetRating(item.Hash, item.Dir, item.Stem, item.Rating)
}

// SetRatingBatch records many ratings in one transaction.
func (s *DecisionService) SetRatingBatch(items []RatingItem) error {
	converted := make([]decide.RatingItem, 0, len(items))
	for _, it := range items {
		item, err := toRatingItem(it)
		if err != nil {
			return err
		}
		converted = append(converted, item)
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetRatingBatch(converted)
}

// SetDestination routes one frame to a folder, or clears its routing when
// destination is empty. Naming a destination implies the frame is worth
// keeping, exactly as toggling a mask does, so an undecided frame becomes a
// keep; a verdict the user has typed is left alone.
func (s *DecisionService) SetDestination(hash, dir, stem, destination, verb string) error {
	item, err := toDestinationItem(DestinationItem{
		Hash: hash, Dir: dir, Stem: stem, Destination: destination, Verb: verb,
	})
	if err != nil {
		return err
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetDestination(item.Hash, item.Dir, item.Stem, item.Destination, item.Verb)
}

// SetDestinationBatch routes many frames in one transaction. Routing a whole
// selection is one keystroke, so it arrives here as one batch.
func (s *DecisionService) SetDestinationBatch(items []DestinationItem) error {
	converted := make([]decide.DestinationItem, 0, len(items))
	for _, it := range items {
		item, err := toDestinationItem(it)
		if err != nil {
			return err
		}
		converted = append(converted, item)
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetDestinationBatch(converted)
}

// Destinations lists the remembered destinations in palette order: pinned
// first, then most recently used, each with the digit that reaches it.
func (s *DecisionService) Destinations() ([]DestinationDTO, error) {
	store, err := s.app.decisions()
	if err != nil {
		return nil, err
	}
	list, err := store.Destinations()
	if err != nil {
		return nil, err
	}
	out := make([]DestinationDTO, 0, len(list))
	for _, d := range list {
		out = append(out, destinationDTO(d))
	}
	return out, nil
}

// UseDestination remembers a destination and moves it to the top of the recent
// list. The palette calls it when a destination is chosen, which is what keeps
// the digit slots following what the user actually does.
func (s *DecisionService) UseDestination(path, label string) error {
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.UseDestination(path, label)
}

// ForgetDestination drops a destination from the palette. Nothing on disk
// moves and no frame already routed there is disturbed.
func (s *DecisionService) ForgetDestination(path string) error {
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.ForgetDestination(path)
}

// PinDestination holds a destination above the recent ones until it is
// unpinned.
func (s *DecisionService) PinDestination(path string, pinned bool) error {
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.PinDestination(path, pinned)
}

// BindDestinationSlot nails a destination to a digit, or releases it with 0.
func (s *DecisionService) BindDestinationSlot(path string, slot int) error {
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.BindSlot(path, slot)
}

// DestinationForDigit resolves what pressing a digit means right now. A digit
// nothing has claimed comes back as a destination with an empty path, which is
// how the palette says "that key does nothing yet" without an error.
func (s *DecisionService) DestinationForDigit(digit int) (DestinationDTO, error) {
	store, err := s.app.decisions()
	if err != nil {
		return DestinationDTO{}, err
	}
	d, ok, err := store.DestinationForDigit(digit)
	if err != nil || !ok {
		return DestinationDTO{}, err
	}
	return destinationDTO(d), nil
}

func destinationDTO(d decide.Destination) DestinationDTO {
	dto := DestinationDTO{
		Path:     d.Path,
		Label:    d.Label,
		UseCount: d.UseCount,
		Pinned:   d.Pinned,
		Slot:     d.Slot,
		Digit:    d.Digit,
	}
	if !d.LastUsedAt.IsZero() {
		dto.LastUsedAt = d.LastUsedAt.Format(time.RFC3339)
	}
	return dto
}

// toDestinationItem validates one incoming destination and converts it for the
// store.
func toDestinationItem(it DestinationItem) (decide.DestinationItem, error) {
	if err := requireHash(it.Hash, it.Stem); err != nil {
		return decide.DestinationItem{}, err
	}
	verb, err := toRouteVerb(it.Verb, it.Stem)
	if err != nil {
		return decide.DestinationItem{}, err
	}
	return decide.DestinationItem{
		Hash:        it.Hash,
		Dir:         it.Dir,
		Stem:        it.Stem,
		Destination: strings.TrimSpace(it.Destination),
		Verb:        verb,
	}, nil
}

// toRouteVerb reads the verb the frontend sent. An empty one is legal and
// means the configured default; anything the store does not know is refused
// here rather than a transaction down, so the caller hears about it before
// half a selection is routed.
func toRouteVerb(verb, stem string) (decide.Verb, error) {
	switch decide.Verb(verb) {
	case decide.VerbDefault:
		return decide.VerbDefault, nil
	case decide.VerbMove:
		return decide.VerbMove, nil
	case decide.VerbCopy:
		return decide.VerbCopy, nil
	}
	return "", fmt.Errorf("unknown route verb %q for %s: want %q or %q",
		verb, stem, decide.VerbMove, decide.VerbCopy)
}

// Set records one decision in the pre-verdict vocabulary. Passing "none"
// clears it. It goes when the grid moves onto verdicts.
func (s *DecisionService) Set(hash, dir, stem, decision string) error {
	item, err := toLegacyItem(DecisionItem{Hash: hash, Dir: dir, Stem: stem, Decision: decision})
	if err != nil {
		return err
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetVerdict(item.Hash, item.Dir, item.Stem, item.Verdict, item.Mask)
}

// SetBatch records many pre-verdict decisions in one transaction.
func (s *DecisionService) SetBatch(items []DecisionItem) error {
	converted := make([]decide.VerdictItem, 0, len(items))
	for _, it := range items {
		item, err := toLegacyItem(it)
		if err != nil {
			return err
		}
		converted = append(converted, item)
	}
	store, err := s.app.decisions()
	if err != nil {
		return err
	}
	return store.SetVerdictBatch(converted)
}

// toVerdictItem validates one incoming verdict and converts it for the store.
func toVerdictItem(it VerdictItem) (decide.VerdictItem, error) {
	v, m, err := parseVerdict(it.Verdict, it.Mask)
	if err != nil {
		return decide.VerdictItem{}, err
	}
	if err := requireHash(it.Hash, it.Stem); err != nil {
		return decide.VerdictItem{}, err
	}
	return decide.VerdictItem{Hash: it.Hash, Dir: it.Dir, Stem: it.Stem, Verdict: v, Mask: m}, nil
}

// toRatingItem validates one incoming rating and converts it for the store.
func toRatingItem(it RatingItem) (decide.RatingItem, error) {
	if it.Rating < 0 || it.Rating > decide.MaxRating {
		return decide.RatingItem{}, fmt.Errorf("rating %d for %q is off the 0-%d scale",
			it.Rating, it.Stem, decide.MaxRating)
	}
	if err := requireHash(it.Hash, it.Stem); err != nil {
		return decide.RatingItem{}, err
	}
	return decide.RatingItem{Hash: it.Hash, Dir: it.Dir, Stem: it.Stem, Rating: it.Rating}, nil
}

// toLegacyItem converts one pre-verdict decision for the store.
func toLegacyItem(it DecisionItem) (decide.VerdictItem, error) {
	r, err := parseDecision(it.Decision)
	if err != nil {
		return decide.VerdictItem{}, err
	}
	if err := requireHash(it.Hash, it.Stem); err != nil {
		return decide.VerdictItem{}, err
	}
	mask := r.Mask
	if mask == "" {
		mask = decide.MaskBoth
	}
	return decide.VerdictItem{Hash: it.Hash, Dir: it.Dir, Stem: it.Stem, Verdict: r.Verdict, Mask: mask}, nil
}

// requireHash refuses to record anything about a frame with no identity: there
// would be no way to find it again.
func requireHash(hash, stem string) error {
	if hash == "" {
		return fmt.Errorf("no frame identity for %q: its decision cannot be recorded", stem)
	}
	return nil
}

// parseVerdict converts a verdict and mask from the frontend, rejecting
// anything the store would not accept. An empty verdict carries no mask,
// because it is on its way to being forgotten.
func parseVerdict(verdict, mask string) (decide.Verdict, decide.Mask, error) {
	v := decide.Verdict(verdict)
	switch v {
	case decide.Undecided:
		if mask != "" {
			if _, err := parseMask(mask); err != nil {
				return "", "", err
			}
		}
		return decide.Undecided, "", nil
	case decide.Keep, decide.Cut:
		m, err := parseMask(mask)
		if err != nil {
			return "", "", err
		}
		return v, m, nil
	}
	return "", "", fmt.Errorf("unknown verdict %q: want %q, %q or an empty string",
		verdict, decide.Keep, decide.Cut)
}

func parseMask(mask string) (decide.Mask, error) {
	switch m := decide.Mask(mask); m {
	case decide.MaskBoth, decide.MaskRAW, decide.MaskJPEG:
		return m, nil
	}
	return "", fmt.Errorf("unknown mask %q: want %q, %q or %q",
		mask, decide.MaskBoth, decide.MaskRAW, decide.MaskJPEG)
}

// legacyDecisions maps the pre-verdict decision names onto the records they
// mean. Both directions of this table exist only so the current grid keeps
// working while it is restyled onto verdicts, masks and ratings.
var legacyDecisions = map[string]decide.Record{
	"none":      {},
	"keep_all":  {Verdict: decide.Keep, Mask: decide.MaskBoth},
	"drop_raw":  {Verdict: decide.Keep, Mask: decide.MaskJPEG},
	"drop_jpeg": {Verdict: decide.Keep, Mask: decide.MaskRAW},
	"drop_all":  {Verdict: decide.Cut, Mask: decide.MaskBoth},
}

// parseDecision converts a pre-verdict decision string into the record it
// stands for.
func parseDecision(s string) (decide.Record, error) {
	r, ok := legacyDecisions[s]
	if !ok {
		return decide.Record{}, fmt.Errorf("unknown decision %q: want none, keep_all, drop_raw, drop_jpeg or drop_all", s)
	}
	return r, nil
}

// legacyDecision names a record in the pre-verdict vocabulary. A cut is a
// drop_all whatever its mask says, since the old model had no way to describe
// a partial cut, and a rating on its own was not a decision at all.
func legacyDecision(r decide.Record) string {
	switch r.Verdict {
	case decide.Cut:
		return "drop_all"
	case decide.Keep:
		switch r.Mask {
		case decide.MaskJPEG:
			return "drop_raw"
		case decide.MaskRAW:
			return "drop_jpeg"
		}
		return "keep_all"
	}
	return "none"
}
