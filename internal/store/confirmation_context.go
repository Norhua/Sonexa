package store

import (
	"encoding/json"

	"github.com/norhua/sonexa/internal/matching"
)

// Persisted at the same instant as acceptance, alongside the raw input and
// binding key. This is a baseline, not an alias guessed from today's candidate.
type confirmationContext struct {
	Version    int             `json:"version"`
	BindingKey string          `json:"bindingKey"`
	Current    matching.Input  `json:"current"`
	Snapshot   *matching.Input `json:"snapshot,omitempty"`
}

func trackConfirmationContext(track ownedImportTrack) confirmationContext {
	c := confirmationContext{Version: 1, BindingKey: track.bindingKey, Current: track.current}
	if track.snapshotValid {
		c.Snapshot = &track.snapshot
	}
	return c
}

func confirmedContextCompatible(c confirmedImport, track ownedImportTrack) bool {
	if c.context == nil {
		// Pre-migration evidence cannot justify a previously unspecified version.
		// Keep the legacy conservative rule, including its exact position key.
		if c.bindingKey != track.bindingKey {
			return false
		}
		compatible := func(view matching.Input) bool {
			return matching.ImportVersionCompatible(importInput(c.input), view) && matching.CompatibleImportPosition(importInput(c.input), view)
		}
		return compatible(track.current) || (track.snapshotValid && compatible(track.snapshot))
	}
	baseline := c.context
	if baseline.Version != 1 || baseline.BindingKey != c.bindingKey || !confirmationBindingCompatible(c.bindingKey, track.bindingKey) {
		return false
	}
	if !matching.ConfirmedTargetUnchanged(baseline.Current, track.current) {
		return false
	}
	if track.snapshotValid {
		approved := baseline.Snapshot
		if approved == nil {
			approved = &baseline.Current
		}
		if !matching.ConfirmedTargetUnchanged(*approved, track.snapshot) {
			return false
		}
	} else if baseline.Snapshot != nil {
		return false
	}
	// New facts may be learned only if they do not contradict the ORIGINAL
	// approved input's known position/duration. Version ambiguity was resolved
	// by the human and is checked against the saved target above instead.
	return matching.CompatibleImportPosition(importInput(c.input), track.current) &&
		(!track.snapshotValid || matching.CompatibleImportPosition(importInput(c.input), track.snapshot))
}

// Identity components remain exact. Unknown positions can become known, but a
// previously known position cannot change or disappear. Triggers also enforce
// this so an edit followed by reverting it cannot revive an invalid relation.
func confirmationBindingCompatible(before, after string) bool {
	var a, b []json.RawMessage
	if json.Unmarshal([]byte(before), &a) != nil || json.Unmarshal([]byte(after), &b) != nil || len(a) != 7 || len(b) != 7 {
		return false
	}
	for i := 0; i < 5; i++ {
		if string(a[i]) != string(b[i]) {
			return false
		}
	}
	for i := 5; i < 7; i++ {
		if string(a[i]) != "null" && string(a[i]) != string(b[i]) {
			return false
		}
	}
	return true
}
