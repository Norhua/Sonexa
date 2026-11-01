package app

import "github.com/google/uuid"

// fixtureMBID gives named synthetic fixtures deterministic valid native IDs.
// It is not used by production normalization or identity decisions.
func fixtureMBID(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("sonexa-fixture:"+name)).String()
}
