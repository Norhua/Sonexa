package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

var _ matching.LocalRecall = (*Store)(nil)
var _ matching.LocalFactLookup = (*Store)(nil)

// ReadLocalFacts complements text recall with exact identities already supplied
// by a source. It neither resolves identity nor assigns ownership. Both fact
// reads and binding lookup share a transaction, independent of display limits.
func (s *Store) ReadLocalFacts(ctx context.Context, identities []domain.Identity) ([]matching.LocalRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []matching.LocalRecord{}
	if len(identities) == 0 {
		return out, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	seen := map[domain.Identity]bool{}
	for _, identity := range identities {
		if seen[identity] || identity.Provider == "" || identity.Ref == "" {
			continue
		}
		seen[identity] = true
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM track WHERE match_source=? AND match_ref=?", identity.Provider, identity.Ref).Scan(&id)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		record, err := readLocalRecordTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// RecallLocal reads a bounded candidate pool for each input, independent of the
// UI's ListTracks cap. Keys are only a blocking index; full facts stay separate.
// Dirty-key refresh and fact reads share a transaction, with no network or files.
func (s *Store) RecallLocal(ctx context.Context, inputs []matching.InputProfile, limit int) ([]matching.LocalRecallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inputs) > 128 {
		return nil, fmt.Errorf("本地召回一次最多 128 个输入")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	out := make([]matching.LocalRecallResult, len(inputs))
	if len(inputs) == 0 {
		return out, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := refreshLocalMatchTx(ctx, tx, false); err != nil {
		return nil, err
	}
	cache := map[int64]matching.LocalRecord{}
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// JSON table parameters avoid SQLite's expression-depth/variable limits
		// when a profile carries many explicit aliases; no key is silently lost.
		keys := []localRecallQueryKey{}
		for _, field := range []struct {
			name string
			text matching.TextProfile
		}{{"title", input.Title}, {"artist", input.Artist.Text}, {"album", input.Album}} {
			for _, key := range matching.RecallTextForms(field.text) {
				keys = append(keys, localRecallQueryKey{Field: field.name, Name: key, Prefix: field.text.Truncated})
			}
		}
		out[i].Records = []matching.LocalRecord{}
		if len(keys) == 0 {
			continue
		}
		encoded, err := json.Marshal(keys)
		if err != nil {
			return nil, err
		}
		rows, err := tx.QueryContext(ctx, `
 WITH query_keys AS (
  SELECT json_extract(value,'$.field') AS field,json_extract(value,'$.name') AS name,
         json_extract(value,'$.prefix') AS prefix FROM json_each(?)
 )
 SELECT track_id FROM (
  SELECT m.track_id FROM query_keys q JOIN local_match_key m ON m.field=q.field AND m.name_key=q.name WHERE NOT q.prefix
  UNION
  SELECT m.track_id FROM query_keys q JOIN local_match_key m ON m.field=q.field AND m.name_key>=q.name AND m.name_key<q.name||char(1114111) WHERE q.prefix
 ) ORDER BY track_id LIMIT ?`, string(encoded), limit+1)
		if err != nil {
			return nil, err
		}
		ids, err := readRecallIDs(rows)
		if err != nil {
			return nil, err
		}
		out[i].Truncated = len(ids) > limit
		if out[i].Truncated {
			ids = ids[:limit]
		}
		for _, id := range ids {
			record, ok := cache[id]
			if !ok {
				record, err = readLocalRecordTx(ctx, tx, id)
				if err != nil {
					return nil, err
				}
				cache[id] = record
			}
			out[i].Records = append(out[i].Records, record)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

type localRecallQueryKey struct {
	Field  string `json:"field"`
	Name   string `json:"name"`
	Prefix bool   `json:"prefix"`
}

// RebuildLocalMatch discards all derived keys. No historical import or selection
// record is consulted; changing MetadataRuleVersion also causes this rebuild.
func (s *Store) RebuildLocalMatch(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := refreshLocalMatchTx(ctx, tx, true); err != nil {
		return err
	}
	return tx.Commit()
}

func readRecallIDs(rows *sql.Rows) ([]int64, error) {
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func refreshLocalMatchTx(ctx context.Context, tx *sql.Tx, force bool) error {
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT rule_version FROM local_match_state WHERE id=1").Scan(&version); err != nil {
		return err
	}
	if force || version != matching.MetadataRuleVersion {
		if _, err := tx.ExecContext(ctx, "DELETE FROM local_match_key; INSERT OR IGNORE INTO local_match_dirty SELECT id FROM track"); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT track_id FROM local_match_dirty ORDER BY track_id")
	if err != nil {
		return err
	}
	ids, err := readRecallIDs(rows)
	if err != nil {
		return err
	}
	for _, id := range ids {
		record, err := readLocalRecordTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM local_match_key WHERE track_id=?", id); err != nil {
			return err
		}
		for _, entry := range localRecordTexts(record) {
			profile := matching.ParseMetadataText(entry.text, matching.MetadataField(entry.field))
			body, err := json.Marshal(profile)
			if err != nil {
				return err
			}
			for _, key := range matching.RecallTextForms(profile) {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO local_match_key(track_id,field,source,name_key,profile_json,rule_version) VALUES(?,?,?,?,?,?)`, id, entry.field, entry.source, key, string(body), matching.MetadataRuleVersion); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM local_match_dirty WHERE track_id=?", id); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE local_match_state SET rule_version=? WHERE id=1", matching.MetadataRuleVersion)
	return err
}

type localRecallText struct{ field, source, text string }

func localRecordTexts(r matching.LocalRecord) []localRecallText {
	out := []localRecallText{{"title", "current", r.Current.Profile.Title.Raw}, {"artist", "current", r.Current.Profile.Artist.Text.Raw}, {"album", "current", r.Current.Profile.Album.Raw}, {"artist", "current_release", r.Current.ReleaseCredit.Text}}
	credit := func(source string, c domain.Credit) {
		out = append(out, localRecallText{"artist", source, c.Text})
		for _, m := range c.Members {
			out = append(out, localRecallText{"artist", source, m.Artist.Name}, localRecallText{"artist", source, m.CreditedName})
		}
	}
	credit("current_credit", r.Current.TrackCredit)
	credit("current_release_credit", r.Current.ReleaseCredit)
	for _, role := range r.Current.Roles {
		out = append(out, localRecallText{"artist", "current_role", role.Artist.Name}, localRecallText{"artist", "current_role", role.CreditedName})
	}
	for _, link := range r.Current.ArtistLinks {
		out = append(out, localRecallText{"artist", "current_link", link.From.Name}, localRecallText{"artist", "current_link", link.To.Name})
	}
	if t := r.TrackSnapshot; t != nil {
		if t.Title.State == domain.Value {
			out = append(out, localRecallText{"title", "track_snapshot", t.Title.Value})
		}
		if t.Credit.State == domain.Value {
			credit("track_snapshot", t.Credit.Value)
		}
		for _, field := range t.Roles {
			if field.State == domain.Value {
				for _, role := range field.Value {
					out = append(out, localRecallText{"artist", "track_snapshot_role", role.Artist.Name})
				}
			}
		}
	}
	if release := r.ReleaseSnapshot; release != nil {
		if release.Title.State == domain.Value {
			out = append(out, localRecallText{"album", "release_snapshot", release.Title.Value})
		}
		if release.Credit.State == domain.Value {
			credit("release_snapshot", release.Credit.Value)
		}
		// Only this exact track's entries can recall the collected target. The
		// complete list still accompanies the record for later ambiguity checks.
		if release.Tracklist.State == domain.Value && r.TrackIdentity != nil {
			for _, entry := range release.Tracklist.Value {
				if entry.Track.State != domain.Value || entry.Track.Value != *r.TrackIdentity {
					continue
				}
				if entry.Title.State == domain.Value {
					out = append(out, localRecallText{"title", "release_tracklist", entry.Title.Value})
				}
				if entry.Credit.State == domain.Value {
					credit("release_tracklist", entry.Credit.Value)
				}
			}
		}
	}
	return out
}

func readLocalRecordTx(ctx context.Context, tx *sql.Tx, id int64) (matching.LocalRecord, error) {
	r := matching.LocalRecord{TrackID: id}
	var tp, tr, rp, rr, title, artist, album, releaseCredit, origins, releaseOrigins, trackBody, releaseBody *string
	var year, disc, track *int64
	err := tx.QueryRowContext(ctx, `SELECT t.revision,t.match_source,t.match_ref,COALESCE(t.release_id,0),
 COALESCE(r.revision,0),r.provider,r.provider_ref,COALESCE(r.release_group_id,0),COALESCE(g.revision,0),
 c.title,c.credit,c.year,c.disc_number,c.track_number,c.states_json,rc.title,rc.credit,rc.states_json,
 ts.body,COALESCE(ts.revision,0),rs.body,COALESCE(rs.revision,0)
 FROM track t JOIN track_current c ON c.track_id=t.id
 LEFT JOIN release r ON r.id=t.release_id LEFT JOIN release_group g ON g.id=r.release_group_id
 LEFT JOIN release_current rc ON rc.release_id=r.id LEFT JOIN track_snapshot ts ON ts.track_id=t.id
 LEFT JOIN release_snapshot rs ON rs.release_id=r.id WHERE t.id=?`, id).Scan(
		&r.TrackRevision, &tp, &tr, &r.ReleaseID, &r.ReleaseRevision, &rp, &rr, &r.GroupID, &r.GroupRevision,
		&title, &artist, &year, &disc, &track, &origins, &album, &releaseCredit, &releaseOrigins,
		&trackBody, &r.TrackSnapshotRevision, &releaseBody, &r.ReleaseSnapshotRevision)
	if err != nil {
		return r, err
	}
	r.TrackIdentity, r.ReleaseIdentity = recallIdentity(tp, tr), recallIdentity(rp, rr)
	r.Current.Profile = matching.NewInputProfile(deref(title), deref(artist), deref(album))
	for _, field := range []struct {
		number *int64
		out    *matching.NumberInput
	}{{year, &r.Current.Profile.Year}, {disc, &r.Current.Profile.Disc}, {track, &r.Current.Profile.Track}} {
		if field.number != nil {
			field.out.State, field.out.Value, field.out.Precision = matching.ValueKnown, *field.number, 1
		}
	}
	r.Current.Origins = map[string]matching.LocalFieldOrigin{}
	for _, layer := range []struct {
		prefix string
		raw    *string
	}{{"track.", origins}, {"release.", releaseOrigins}} {
		if layer.raw == nil {
			continue
		}
		values := map[string]matching.LocalFieldOrigin{}
		if err := json.Unmarshal([]byte(*layer.raw), &values); err != nil {
			return r, err
		}
		for key, origin := range values {
			r.Current.Origins[layer.prefix+key] = origin
		}
	}
	if trackBody != nil {
		var snapshot domain.TrackSnapshot
		if err := json.Unmarshal([]byte(*trackBody), &snapshot); err != nil {
			return r, err
		}
		if r.TrackIdentity != nil && snapshot.Identity == *r.TrackIdentity &&
			(snapshot.Release == nil || (r.ReleaseIdentity != nil && *snapshot.Release == *r.ReleaseIdentity)) {
			r.TrackSnapshot = &snapshot
		} else {
			r.Issues = append(r.Issues, matching.DecisionReason{Code: "stale_track_snapshot"})
		}
	}
	if releaseBody != nil {
		var snapshot domain.ReleaseSnapshot
		if err := json.Unmarshal([]byte(*releaseBody), &snapshot); err != nil {
			return r, err
		}
		if r.ReleaseIdentity != nil && snapshot.Identity == *r.ReleaseIdentity {
			r.ReleaseSnapshot = &snapshot
		} else {
			r.Issues = append(r.Issues, matching.DecisionReason{Code: "stale_release_snapshot"})
		}
	}
	if r.GroupID != 0 {
		rows, err := tx.QueryContext(ctx, "SELECT provider,group_ref FROM release_group_ref WHERE group_id=? ORDER BY provider,group_ref", r.GroupID)
		if err != nil {
			return r, err
		}
		for rows.Next() {
			var identity domain.Identity
			if err := rows.Scan(&identity.Provider, &identity.Ref); err != nil {
				rows.Close()
				return r, err
			}
			r.GroupIdentities = append(r.GroupIdentities, identity)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return r, err
		}
	}
	if err := readLocalRelationsTx(ctx, tx, &r); err != nil {
		return r, err
	}
	return r, nil
}

func recallIdentity(provider, ref *string) *domain.Identity {
	if provider == nil || ref == nil || *provider == "" || *ref == "" {
		return nil
	}
	return &domain.Identity{Provider: domain.ProviderID(*provider), Ref: *ref}
}

func readLocalRelationsTx(ctx context.Context, tx *sql.Tx, r *matching.LocalRecord) error {
	r.Current.TrackCredit.Text, r.Current.ReleaseCredit.Text = r.Current.Profile.Artist.Text.Raw, ""
	if r.ReleaseID != 0 {
		var text *string
		if err := tx.QueryRowContext(ctx, "SELECT credit FROM release_current WHERE release_id=?", r.ReleaseID).Scan(&text); err != nil {
			return err
		}
		r.Current.ReleaseCredit.Text = deref(text)
	}
	artists := map[int64]bool{}
	for _, layer := range []struct {
		table, key string
		id         int64
		credit     *domain.Credit
	}{{"track_credit", "track_id", r.TrackID, &r.Current.TrackCredit}, {"release_credit", "release_id", r.ReleaseID, &r.Current.ReleaseCredit}} {
		rows, err := tx.QueryContext(ctx, `SELECT a.id,a.name,COALESCE(a.sort_name,''),COALESCE(a.type,''),a.provider,a.provider_ref,COALESCE(c.credited_name,''),COALESCE(c.span_start,0),COALESCE(c.span_end,0)
 FROM `+layer.table+` c JOIN artist a ON a.id=c.artist_id WHERE c.`+layer.key+`=? ORDER BY c.position,a.id`, layer.id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var member domain.CreditMember
			var id int64
			var p, ref *string
			if err := rows.Scan(&id, &member.Artist.Name, &member.Artist.SortName, &member.Artist.Type, &p, &ref, &member.CreditedName, &member.Start, &member.End); err != nil {
				rows.Close()
				return err
			}
			member.Artist.Identity = recallIdentity(p, ref)
			layer.credit.Members = append(layer.credit.Members, member)
			artists[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.name,COALESCE(a.sort_name,''),COALESCE(a.type,''),a.provider,a.provider_ref,ta.role,ta.attributes_json,ta.position,ta.source
 FROM track_artist ta JOIN artist a ON a.id=ta.artist_id WHERE ta.track_id=? ORDER BY ta.role,ta.position,a.id`, r.TrackID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var role domain.Role
		var id int64
		var p, ref, source *string
		var attributes string
		if err := rows.Scan(&id, &role.Artist.Name, &role.Artist.SortName, &role.Artist.Type, &p, &ref, &role.Role, &attributes, &role.Position, &source); err != nil {
			rows.Close()
			return err
		}
		// A stale followed relation is not current evidence for a new binding.
		origin := r.Current.Origins["track.roles."+role.Role]
		if origin.Mode == domain.Follow && deref(source) != CVCreditSource &&
			(r.TrackSnapshot == nil || deref(source) != string(r.TrackSnapshot.Identity.Provider)) {
			continue
		}
		if err := json.Unmarshal([]byte(attributes), &role.Attributes); err != nil {
			rows.Close()
			return err
		}
		role.Artist.Identity = recallIdentity(p, ref)
		r.Current.Roles = append(r.Current.Roles, role)
		artists[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(artists) == 0 {
		return err
	}
	ids := make([]int64, 0, len(artists))
	for id := range artists {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	var args []any
	for repeat := 0; repeat < 2; repeat++ {
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT a.name,COALESCE(a.sort_name,''),COALESCE(a.type,''),a.provider,a.provider_ref,
 b.name,COALESCE(b.sort_name,''),COALESCE(b.type,''),b.provider,b.provider_ref,l.type
 FROM artist_link l JOIN artist a ON a.id=l.from_artist_id JOIN artist b ON b.id=l.to_artist_id
 WHERE l.from_artist_id IN (`+placeholders+`) OR l.to_artist_id IN (`+placeholders+`) ORDER BY l.from_artist_id,l.to_artist_id,l.type`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var link matching.LocalArtistLink
		var ap, ar, bp, br *string
		if err := rows.Scan(&link.From.Name, &link.From.SortName, &link.From.Type, &ap, &ar, &link.To.Name, &link.To.SortName, &link.To.Type, &bp, &br, &link.Kind); err != nil {
			return err
		}
		link.From.Identity, link.To.Identity = recallIdentity(ap, ar), recallIdentity(bp, br)
		r.Current.ArtistLinks = append(r.Current.ArtistLinks, link)
	}
	return rows.Err()
}
