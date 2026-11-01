package matching

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const ProblemProofVersion = 1

type ProblemProofKind string

const (
	ProblemIndependent ProblemProofKind = "independent"
	ProblemStrongText  ProblemProofKind = "strong_text"
	ProblemExactTrack  ProblemProofKind = "exact_track"
)

// ProblemInput contains current evidence, not a historical choice or a list of
// acceptable candidates. Pool is optional; no-result inputs can use strong text.
// A chosen/preferred track alone is intentionally not part of this protocol.
type ProblemInput struct {
	Profile InputProfile   `json:"profile"`
	Pool    *CandidatePool `json:"pool,omitempty"`
}

type ProblemProof struct {
	Version     int                 `json:"version"`
	RuleVersion int                 `json:"ruleVersion"`
	Kind        ProblemProofKind    `json:"kind"`
	Inputs      []string            `json:"inputs"` // hashes bind proof to whole evidence views
	Names       map[string][]string `json:"names,omitempty"`
	Target      *CandidateIdentity  `json:"target,omitempty"`
	Reasons     []DecisionReason    `json:"reasons,omitempty"`
}

type ProblemRecallKey struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Anchor retains every current member's evidence. Extensions re-prove the
// entire group with a common witness; they never union alias sets or take the
// transitive closure of pairwise compatibility. These are active-work facts.
type ProblemAnchor struct {
	Version    int                `json:"version"`
	Isolated   bool               `json:"isolated,omitempty"` // explicit current-work split, never a historical identity rule
	Inputs     []ProblemInput     `json:"inputs"`
	RecallKeys []ProblemRecallKey `json:"recallKeys"`
}

func NewProblemAnchor(inputs ...ProblemInput) (ProblemAnchor, ProblemProof) {
	return makeProblemAnchor(inputs)
}

func ExtendProblemAnchor(anchor ProblemAnchor, input ProblemInput) (ProblemAnchor, ProblemProof, bool) {
	if anchor.Version != ProblemProofVersion || anchor.Isolated || len(anchor.Inputs) == 0 {
		return ProblemAnchor{}, ProblemProof{}, false
	}
	inputs := append(append([]ProblemInput(nil), anchor.Inputs...), input)
	next, proof := makeProblemAnchor(inputs)
	return next, proof, proof.Kind != ProblemIndependent
}

func makeProblemAnchor(inputs []ProblemInput) (ProblemAnchor, ProblemProof) {
	// Detached values: callers must not mutate a saved proof through shared
	// slices, maps or candidate pointers while later inputs are evaluated.
	body, err := json.Marshal(inputs)
	if err != nil {
		return ProblemAnchor{}, ProblemProof{Version: ProblemProofVersion, Kind: ProblemIndependent}
	}
	var copied []ProblemInput
	if json.Unmarshal(body, &copied) != nil {
		return ProblemAnchor{}, ProblemProof{Version: ProblemProofVersion, Kind: ProblemIndependent}
	}
	anchor := ProblemAnchor{Version: ProblemProofVersion, Inputs: copied}
	seen := map[ProblemRecallKey]bool{}
	for _, input := range copied {
		for _, key := range ProblemRecallKeys(input) {
			if !seen[key] {
				seen[key] = true
				anchor.RecallKeys = append(anchor.RecallKeys, key)
			}
		}
	}
	sort.Slice(anchor.RecallKeys, func(i, j int) bool {
		a, b := anchor.RecallKeys[i], anchor.RecallKeys[j]
		return a.Kind < b.Kind || a.Kind == b.Kind && a.Value < b.Value
	})
	return anchor, ProveProblem(copied)
}

// ProveProblem uses one proof for ALL inputs. The identity path is independent
// of text equivalence but still rechecks each input against its full current
// fact pool. A default edition preference cannot manufacture an exact proof.
func ProveProblem(inputs []ProblemInput) ProblemProof {
	proof := ProblemProof{Version: ProblemProofVersion, RuleVersion: MetadataRuleVersion, Kind: ProblemIndependent}
	fail := func(code string) ProblemProof {
		proof.Reasons = append(proof.Reasons, DecisionReason{Code: code})
		return proof
	}
	if len(inputs) == 0 {
		return fail("empty_problem")
	}
	var target *CandidateIdentity
	allExact := true
	for _, input := range inputs {
		body, err := json.Marshal(input)
		if err != nil {
			return fail("invalid_problem_evidence")
		}
		hash := sha256.Sum256(body)
		proof.Inputs = append(proof.Inputs, hex.EncodeToString(hash[:]))
		if input.Profile.RuleVersion != MetadataRuleVersion || !problemNumbersValid(input.Profile) {
			return fail("invalid_problem_profile")
		}
		next := ExactProblemTarget(input)
		if next == nil {
			allExact = false
			continue
		}
		if target != nil && (target.Track != next.Track || target.Release != next.Release) {
			return fail("different_exact_problem_targets")
		}
		target = next
	}
	if allExact {
		// The same ID in two contradictory source views is not a shared
		// identity proof. Recheck each input against the union of whole facts,
		// retaining conflicts rather than picking whichever view fits it.
		combined := CandidateTarget{Identity: *target}
		for _, input := range inputs {
			for _, candidate := range input.Pool.Targets {
				if candidate.Identity.Track != target.Track || candidate.Identity.Release != target.Release {
					continue
				}
				for _, fact := range candidate.Facts {
					for _, prior := range combined.Facts {
						combined.Conflicts = append(combined.Conflicts, compareCandidateFacts(prior, fact)...)
					}
					combined.Facts = append(combined.Facts, fact)
				}
			}
		}
		for _, input := range inputs {
			if combined.Check(input.Profile).Eligibility != MetadataEligible {
				return fail("conflicting_exact_problem_facts")
			}
		}
		// Explicit numeric demands may not contradict one another even when
		// both individually fit a duration tolerance around the same track.
		for _, field := range []string{"year", "disc", "track", "duration"} {
			var known *NumberInput
			for _, input := range inputs {
				n := problemNumbers(input.Profile)[field]
				if n.State == ValueMissing {
					continue
				}
				if known != nil && !sameProblemNumber(*known, n) {
					return fail("different_explicit_numeric_requirements")
				}
				copy := n
				known = &copy
			}
		}
		proof.Kind, proof.Target = ProblemExactTrack, target
		return proof
	}
	common := problemTextWitnesses(inputs[0].Profile)
	if common == nil {
		return fail("strong_problem_names_required")
	}
	for _, input := range inputs[1:] {
		if !sameProblemNumbers(inputs[0].Profile, input.Profile) {
			return fail("different_numeric_requirements_or_missing_mask")
		}
		if !strongProblemTextPair(inputs[0].Profile, input.Profile) {
			return fail("problem_names_not_equivalent")
		}
		witnesses := problemTextWitnesses(input.Profile)
		if len(witnesses) != len(common) {
			return fail("different_problem_name_structure")
		}
		for field, names := range common {
			common[field] = intersectProblemNames(names, witnesses[field])
			if len(common[field]) == 0 {
				return fail("no_common_problem_name_witness")
			}
		}
	}
	proof.Kind, proof.Names = ProblemStrongText, common
	return proof
}

func problemNumbers(p InputProfile) map[string]NumberInput {
	return map[string]NumberInput{"year": p.Year, "disc": p.Disc, "track": p.Track, "duration": p.Duration}
}
func problemNumbersValid(p InputProfile) bool {
	for field, n := range problemNumbers(p) {
		unit, semantic := "number", field
		if field == "duration" {
			unit = "ms"
		} else if field == "year" {
			unit, semantic = "year", "unspecified_year"
		}
		if n.Unit != unit || n.Semantic != semantic || n.Error != "" {
			return false
		}
		switch n.State {
		case ValueKnown:
			if n.Value <= 0 {
				return false
			}
		case ValueMissing:
			if n.Value != 0 || n.Raw != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func sameProblemNumber(a, b NumberInput) bool {
	return a.State == b.State && a.Value == b.Value && a.Unit == b.Unit && a.Semantic == b.Semantic
}
func sameProblemNumbers(a, b InputProfile) bool {
	other := problemNumbers(b)
	for field, n := range problemNumbers(a) {
		if !sameProblemNumber(n, other[field]) {
			return false
		}
	}
	return true
}

func strongProblemTextPair(a, b InputProfile) bool {
	ctx := func(field MetadataField, support bool) MetadataTextContext {
		return MetadataTextContext{Field: field, IndependentSupport: support, Source: "active_input"}
	}
	title := CompareMetadataText(a.Title, b.Title, ctx(MetadataTitle, false))
	album := CompareMetadataText(a.Album, b.Album, ctx(MetadataAlbum, false))
	artist := CompareMetadataCredit(a.Artist, b.Artist, MetadataCreditContext{Text: ctx(MetadataArtist, false)})
	return CompareMetadataText(a.Title, b.Title, ctx(MetadataTitle, artist.Reliable() || album.Reliable())).Reliable() &&
		CompareMetadataText(a.Album, b.Album, ctx(MetadataAlbum, title.Reliable() && artist.Reliable())).Reliable() &&
		CompareMetadataCredit(a.Artist, b.Artist, MetadataCreditContext{Text: ctx(MetadataArtist, title.Reliable() && album.Reliable())}).Reliable()
}

func problemTextWitnesses(p InputProfile) map[string][]string {
	if p.Title.Truncated || p.Album.Truncated || p.Artist.Text.Truncated || len(p.Artist.Parts) == 0 {
		return nil
	}
	out := map[string][]string{"title": problemNameWitnesses(p.Title), "album": problemNameWitnesses(p.Album)}
	for i, part := range p.Artist.Parts {
		field := fmt.Sprintf("artist.%d", i)
		out[field] = problemNameWitnesses(ParseMetadataText(part.Name, MetadataArtist))
		out[field+".role"] = []string{part.Role}
		out[field+".voice"] = []string{Normalize(part.Voice)}
	}
	for _, names := range out {
		if len(names) == 0 {
			return nil
		}
	}
	return out
}

func problemNameWitnesses(p TextProfile) []string {
	if p.Truncated || NormalizeKeepBrackets(p.Raw) == "" {
		return nil
	}
	var forms []string
	if len(p.Unknown) > 0 {
		forms = []string{"literal:" + NormalizeKeepBrackets(p.Raw)}
	} else {
		forms = append(forms, "name:"+Normalize(p.Body))
		for _, alias := range p.Aliases {
			if alias.Verified && alias.Text != "" && alias.Basis != "" {
				forms = append(forms, "name:"+Normalize(alias.Text))
			}
		}
	}
	out, seen := []string{}, map[string]bool{}
	for _, form := range forms {
		if form == "name:" || form == "literal:" {
			continue
		}
		body, _ := json.Marshal([]any{form, metadataQualifierKeys(p)})
		key := string(body)
		if !seen[key] {
			out, seen[key] = append(out, key), true
		}
	}
	sort.Strings(out)
	return out
}
func intersectProblemNames(a, b []string) []string {
	out := []string{}
	for _, name := range a {
		i := sort.SearchStrings(b, name)
		if i < len(b) && b[i] == name {
			out = append(out, name)
		}
	}
	return out
}

// ExactProblemTarget re-evaluates the whole current decision scope. It never
// chooses an owned/larger edition or equates recordings with positions.
func ExactProblemTarget(input ProblemInput) *CandidateIdentity {
	if input.Profile.RuleVersion != MetadataRuleVersion || input.Pool == nil || input.Pool.Truncated {
		return nil
	}
	var selected *CandidateIdentity
	for _, target := range input.Pool.Targets {
		evidence := target.Check(input.Profile)
		if evidence.Eligibility == MetadataExcluded {
			continue
		}
		if evidence.Eligibility != MetadataEligible || selected != nil || target.Identity.Track.Ref == "" || target.Identity.Release.Ref == "" || target.Identity.Track.Provider == "" || target.Identity.Release.Provider != target.Identity.Track.Provider {
			return nil
		}
		hasRemote, localPosition := false, false
		for _, fact := range target.Facts {
			c := fact.Metadata.Candidate
			if c.Provider != target.Identity.Track.Provider || c.MatchRef != target.Identity.Track.Ref || deref(c.ReleaseRef) != target.Identity.Release.Ref {
				return nil
			}
			hasRemote = hasRemote || fact.Kind == "remote"
			if fact.Release != nil {
				if !PositionUnique(input.Profile, target, fact.Release) {
					return nil
				}
				localPosition = true
			}
		}
		if !hasRemote && (Normalize(input.Profile.Album.Raw) == "" || !localPosition) {
			return nil
		}
		identity := target.Identity
		selected = &identity
	}
	return selected
}

// ProblemRecallKeys is only conservative blocking. Unknown/partial text can
// retrieve a problem but cannot prove equivalence; absent text adds no key.
// Identity keys come only from a complete proof, never a candidate collection.
func ProblemRecallKeys(input ProblemInput) []ProblemRecallKey {
	seen := map[ProblemRecallKey]bool{}
	out := []ProblemRecallKey{}
	add := func(kind, value string) {
		key := ProblemRecallKey{Kind: kind, Value: value}
		if value != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	for _, field := range []struct {
		name string
		text TextProfile
	}{{"title", input.Profile.Title}, {"album", input.Profile.Album}, {"artist", input.Profile.Artist.Text}} {
		for _, key := range RecallTextForms(field.text) {
			add(field.name, key)
		}
	}
	for _, part := range input.Profile.Artist.Parts {
		for _, key := range RecallTextForms(ParseMetadataText(part.Name, MetadataArtist)) {
			add("artist", key)
		}
	}
	if target := ExactProblemTarget(input); target != nil {
		body, _ := json.Marshal([]any{target.Track, target.Release})
		add("exact_track", string(body))
	}
	return out
}
