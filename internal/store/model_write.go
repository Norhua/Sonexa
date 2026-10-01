package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func insertTrackTx(tx *sql.Tx, in NewTrackInput) (int64, error) {
	releaseID, err := int64(0), error(nil)
	if in.ExistingReleaseID != nil {
		releaseID = *in.ExistingReleaseID
	} else {
		releaseID, err = resolveReleaseTx(tx, in)
	}
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var matchedAt *time.Time
	if in.MatchSource != nil && in.MatchRef != nil {
		matchedAt = &now
	}
	res, err := tx.Exec(`INSERT INTO track(release_id,match_source,match_ref,matched_at,enrich_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, nullableID(releaseID), in.MatchSource, in.MatchRef, matchedAt, string(InitialEnrichState(in)), now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := emptyCurrentTx(tx, "track", id); err != nil {
		return 0, err
	}
	m := in.Metadata
	if m == nil {
		m = &MetadataInput{}
	}
	fields := fieldMap{"title": rawKnown(in.Title), "credit": rawFromPointer(in.Credit), "year": rawFromPointer(m.Year), "discNumber": rawFromPointer(m.DiscNumber), "trackNumber": rawFromPointer(m.TrackNumber), "genre": rawFromPointer(m.Genre)}
	if in.Title == "" {
		fields["title"] = rawField{State: domain.Empty}
	}
	if m.Year == nil && releaseID > 0 {
		var date *string
		if err := tx.QueryRow("SELECT release_date FROM release_read WHERE id=?", releaseID).Scan(&date); err != nil {
			return 0, err
		}
		fields["year"] = rawFromPointer((&Release{ReleaseDate: date}).Year())
	}
	if err := writeCurrentTx(tx, "track", id, fields, deref(in.MatchSource), false, nil); err != nil {
		return 0, err
	}
	snapshot := domain.TrackSnapshot{Title: provider.FieldFromPointer(nonblank(in.Title)), Credit: domain.Missing[domain.Credit](), Year: provider.FieldFromPointer(m.Year), DiscNumber: provider.FieldFromPointer(m.DiscNumber), TrackNumber: provider.FieldFromPointer(m.TrackNumber), TrackNumberText: provider.FieldFromPointer(m.TrackNumberText), Genre: provider.FieldFromPointer(m.Genre), DurationMS: provider.FieldFromPointer(m.DurationMS)}
	if in.Credit != nil {
		snapshot.Credit = domain.Known(domain.Credit{Text: *in.Credit})
	}
	if in.MatchSource != nil {
		snapshot.Meta.Provider = domain.ProviderID(*in.MatchSource)
		snapshot.Identity = domain.Identity{Provider: snapshot.Meta.Provider, Ref: deref(in.MatchRef)}
	}
	if m.RecordingRef != nil {
		snapshot.Recording = domain.Known(domain.Identity{Provider: snapshot.Meta.Provider, Ref: *m.RecordingRef})
	}
	if m.ISRC != nil {
		snapshot.Identifiers = domain.Known([]domain.Identifier{{Kind: "ISRC", Value: *m.ISRC}})
	}
	if m.WorkRef != nil {
		snapshot.Works = domain.Known([]domain.Identity{{Provider: snapshot.Meta.Provider, Ref: *m.WorkRef}})
	}
	if in.MatchSource != nil && in.MatchRef != nil {
		if _, err := saveSnapshotTx(tx, "track", id, snapshot); err != nil {
			return 0, err
		}
		if err := rebuildIdentifiersTx(tx, id, snapshot); err != nil {
			return 0, err
		}
	}
	if err := refreshCreditsTx(tx, "track", id); err != nil {
		return 0, err
	}
	if err := syncFTS(tx, id); err != nil {
		return 0, err
	}
	return id, nil
}
func nonblank(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func createGroupTx(tx *sql.Tx, title string, credit *string) (int64, error) {
	now := time.Now().UTC()
	res, err := tx.Exec("INSERT INTO release_group(created_at,updated_at) VALUES(?,?)", now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := emptyCurrentTx(tx, "release_group", id); err != nil {
		return 0, err
	}
	err = writeCurrentTx(tx, "release_group", id, fieldMap{"title": rawFromPointer(nonblank(title)), "credit": rawFromPointer(credit)}, "", false, nil)
	return id, err
}
func writeLegacyGroupTx(tx *sql.Tx, g *ReleaseGroup) (int64, error) {
	if g.Provider == nil || g.ProviderRef == nil || *g.ProviderRef == "" {
		return 0, fmt.Errorf("发行组需要来源身份")
	}
	var id int64
	err := tx.QueryRow("SELECT group_id FROM release_group_ref WHERE provider=? AND group_ref=?", g.Provider, g.ProviderRef).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == sql.ErrNoRows {
		id, err = createGroupTx(tx, "", nil)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec("INSERT INTO release_group_ref(group_id,provider,group_ref) VALUES(?,?,?)", id, g.Provider, g.ProviderRef); err != nil {
			return 0, err
		}
	}
	snapshot := domain.GroupSnapshot{Identity: domain.Identity{Provider: domain.ProviderID(*g.Provider), Ref: *g.ProviderRef}, Title: provider.FieldFromPointer(nonblank(g.Title)), PrimaryType: provider.FieldFromPointer(g.PrimaryType), FirstReleaseDate: provider.FieldFromPointer(g.FirstReleaseDate)}
	if err := saveGroupSnapshotTx(tx, id, &snapshot); err != nil {
		return 0, err
	}
	if g.ReleaseCount != nil {
		if _, err := tx.Exec("UPDATE release_group_snapshot SET release_count=? WHERE provider=? AND group_ref=?", *g.ReleaseCount, g.Provider, g.ProviderRef); err != nil {
			return 0, err
		}
	}

	return id, nil
}

func writeLegacyReleaseTx(tx *sql.Tx, r *Release) (int64, error) {
	var id int64
	if r.ProviderRef != nil && r.Provider != nil {
		err := tx.QueryRow("SELECT id FROM release WHERE provider=? AND provider_ref=?", r.Provider, r.ProviderRef).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
	}
	if id == 0 {
		group := int64(0)
		if r.ReleaseGroupID != nil {
			group = *r.ReleaseGroupID
		}
		if group == 0 {
			var err error
			group, err = createGroupTx(tx, r.Title, r.Credit)
			if err != nil {
				return 0, err
			}
		}
		now := time.Now().UTC()
		res, err := tx.Exec(`INSERT INTO release(release_group_id,provider,provider_ref,tracklist_state,fetched_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, group, r.Provider, r.ProviderRef, stateOrNone(r.TracklistState), r.FetchedAt, now, now)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
		if err := emptyCurrentTx(tx, "release", id); err != nil {
			return 0, err
		}
	}
	if r.TracklistState != "" && r.TracklistState != TracklistNone {
		if _, err := tx.Exec("UPDATE release SET tracklist_state=CASE WHEN tracklist_state='complete' AND ?='partial' THEN tracklist_state ELSE ? END,fetched_at=COALESCE(?,fetched_at),revision=revision+1 WHERE id=?", r.TracklistState, r.TracklistState, r.FetchedAt, id); err != nil {
			return 0, err
		}
	}
	fields := fieldMap{"title": rawFromPointer(nonblank(r.Title)), "credit": rawFromPointer(r.Credit), "primaryType": rawFromPointer(r.PrimaryType), "discTotal": rawFromPointer(r.DiscTotal)}
	if r.SecondaryTypes != nil {
		fields["secondaryTypes"] = rawField{State: domain.Value, Value: json.RawMessage(*r.SecondaryTypes)}
	}
	if err := writeCurrentTx(tx, "release", id, fields, deref(r.Provider), false, nil); err != nil {
		return 0, err
	}
	snap := domain.ReleaseSnapshot{Identity: domain.Identity{Provider: domain.ProviderID(deref(r.Provider)), Ref: deref(r.ProviderRef)}, Title: provider.FieldFromPointer(nonblank(r.Title)), PrimaryType: provider.FieldFromPointer(r.PrimaryType), DiscTotal: provider.FieldFromPointer(r.DiscTotal), ReleaseTrackTotal: provider.FieldFromPointer(r.TrackTotal), Country: provider.FieldFromPointer(r.Country), Date: provider.FieldFromPointer(r.ReleaseDate), Status: provider.FieldFromPointer(r.ReleaseStatus)}
	if r.Credit != nil {
		snap.Credit = domain.Known(domain.Credit{Text: *r.Credit})
	}
	if r.Label != nil || r.CatalogNumber != nil {
		snap.Labels = domain.Known([]domain.Label{{Name: deref(r.Label), CatalogNumber: deref(r.CatalogNumber)}})
	}
	if r.MediumFormat != nil {
		snap.Media = domain.Known([]domain.Medium{{Number: 1, Format: domain.Known(*r.MediumFormat)}})
	}
	if r.Provider != nil && r.ProviderRef != nil {
		if _, err := saveSnapshotTx(tx, "release", id, snap); err != nil {
			return 0, err
		}
	}
	return id, nil
}
