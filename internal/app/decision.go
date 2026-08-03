package app

import (
	"fmt"

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
