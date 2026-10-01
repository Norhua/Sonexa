package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

type FieldOrigin struct {
	Mode   domain.ValueMode `json:"mode"`
	Source string           `json:"source"`
}
type rawField struct {
	State domain.FieldState `json:"state"`
	Value json.RawMessage   `json:"value,omitempty"`
}
type fieldMap map[string]rawField

var currentColumns = map[string]map[string]string{
	"track":         {"title": "title", "credit": "credit", "year": "year", "discNumber": "disc_number", "trackNumber": "track_number", "genre": "genre", "lyricsFileId": "lyrics_file_id"},
	"release":       {"title": "title", "credit": "credit", "primaryType": "primary_type", "secondaryTypes": "secondary_types", "discTotal": "disc_total", "media": "media_json"},
	"release_group": {"title": "title", "credit": "credit", "primaryType": "primary_type", "secondaryTypes": "secondary_types"},
}

func entityKey(kind string) string {
	if kind == "release_group" {
		return "group_id"
	}
	return kind + "_id"
}
func rawKnown(value any) rawField {
	b, _ := json.Marshal(value)
	return rawField{State: domain.Value, Value: b}
}
func rawFromPointer[T any](value *T) rawField {
	if value == nil {
		return rawField{State: domain.Unknown}
	}
	return rawKnown(*value)
}
func layerFields(layer any) (fieldMap, error) {
	b, err := json.Marshal(layer)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	out := fieldMap{}
	for key, value := range fields {
		var f rawField
		if json.Unmarshal(value, &f) == nil && f.State != "" {
			out[key] = f
		}
	}
	if f := out["credit"]; f.State == domain.Value {
		var c domain.Credit
		if err := json.Unmarshal(f.Value, &c); err != nil {
			return nil, err
		}
		out["credit"] = rawKnown(c.Text)
	}
	return out, nil
}
func readOriginsTx(tx *sql.Tx, kind string, id int64) (map[string]FieldOrigin, error) {
	var raw string
	if err := tx.QueryRow("SELECT states_json FROM "+kind+"_current WHERE "+entityKey(kind)+"=?", id).Scan(&raw); err != nil {
		return nil, err
	}
	out := map[string]FieldOrigin{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
func fieldSQLValue(f rawField, column string) (any, error) {
	if f.State == domain.Empty {
		return nil, nil
	}
	if column == "media_json" || column == "secondary_types" {
		return string(f.Value), nil
	}
	var v any
	if err := json.Unmarshal(f.Value, &v); err != nil {
		return nil, err
	}
	return v, nil
}
func rawEqual(a, b rawField) bool {
	if a.State != b.State {
		return false
	}
	if a.State == domain.Empty {
		return true
	}
	var x, y any
	_ = json.Unmarshal(a.Value, &x)
	_ = json.Unmarshal(b.Value, &y)
	return reflect.DeepEqual(x, y)
}

// boundSnapshotTx 只允许与当前绑定的快照比较，旧身份或本地值不能充当上游事实。
func boundSnapshotTx(tx *sql.Tx, kind string, id int64) (fieldMap, string, error) {
	if kind == "release_group" {
		return nil, "", nil
	}
	var body, source, ref *string
	identityColumns := "e.provider,e.provider_ref"
	if kind == "track" {
		identityColumns = "e.match_source,e.match_ref"
	}
	err := tx.QueryRow("SELECT s.body,"+identityColumns+" FROM "+kind+" e LEFT JOIN "+kind+"_snapshot s ON s."+entityKey(kind)+"=e.id WHERE e.id=?", id).Scan(&body, &source, &ref)
	if err != nil {
		return nil, "", err
	}
	if body == nil || source == nil || ref == nil {
		return nil, "", nil
	}
	var snapshot struct {
		Identity domain.Identity `json:"identity"`
	}
	if err := json.Unmarshal([]byte(*body), &snapshot); err != nil {
		return nil, "", err
	}
	if snapshot.Identity.Provider != domain.ProviderID(*source) || snapshot.Identity.Ref != *ref {
		return nil, "", nil
	}
	fields, err := layerFields(json.RawMessage(*body))
	if kind == "track" {
		var lyricsID *int64
		var state domain.FieldState
		if err := tx.QueryRow("SELECT lyrics_file_id,lyrics_state FROM track_snapshot WHERE track_id=?", id).Scan(&lyricsID, &state); err != nil {
			return nil, "", err
		}
		if lyricsID != nil {
			fields["lyricsFileId"] = rawKnown(*lyricsID)
		} else if state == domain.Empty {
			fields["lyricsFileId"] = rawField{State: domain.Empty}
		}
	}
	return fields, *source, err
}

// writeCurrentTx 是当前字段唯一写入规则；自动同步保护 user，显式编辑与有效快照比较。
func writeCurrentTx(tx *sql.Tx, kind string, id int64, fields fieldMap, source string, editing bool, snapshot fieldMap) error {
	columns, ok := currentColumns[kind]
	if !ok {
		return fmt.Errorf("未知实体层级 %s", kind)
	}
	origins, err := readOriginsTx(tx, kind, id)
	if err != nil {
		return err
	}
	if editing {
		snapshot, source, err = boundSnapshotTx(tx, kind, id)
		if err != nil {
			return err
		}
	}
	var sets []string
	var args []any
	for key, f := range fields {
		column, ok := columns[key]
		if !ok || (f.State != domain.Value && f.State != domain.Empty) {
			continue
		}
		origin := origins[key]
		if !editing && origin.Mode == domain.User {
			continue
		}
		mode := domain.Follow
		if source == "" || (editing && !rawEqual(f, snapshot[key])) {
			mode = domain.User
		}
		fieldSource := source
		if mode == domain.User {
			fieldSource = "user"
		}
		v, err := fieldSQLValue(f, column)
		if err != nil {
			return err
		}
		sets = append(sets, column+"=?")
		args = append(args, v)
		origins[key] = FieldOrigin{Mode: mode, Source: fieldSource}
	}
	if len(sets) == 0 {
		return nil
	}
	b, err := json.Marshal(origins)
	if err != nil {
		return err
	}
	sets = append(sets, "states_json=?")
	args = append(args, string(b), id)
	if _, err := tx.Exec("UPDATE "+kind+"_current SET "+strings.Join(sets, ",")+" WHERE "+entityKey(kind)+"=?", args...); err != nil {
		return err
	}
	if _, changed := fields["credit"]; changed {
		if err := refreshCreditsTx(tx, kind, id); err != nil {
			return err
		}
	}
	_, err = tx.Exec("UPDATE "+kind+" SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UTC(), id)
	return err
}

func mergeSnapshotJSON(old, incoming []byte) ([]byte, error) {
	previous := map[string]json.RawMessage{}
	next := map[string]json.RawMessage{}
	if len(old) > 0 {
		if err := json.Unmarshal(old, &previous); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(incoming, &next); err != nil {
		return nil, err
	}
	for key, value := range next {
		if key == "tracklistCompleteness" && (string(value) == `"unknown"` || (string(previous[key]) == `"complete"` && string(value) == `"partial"`)) {
			continue
		}
		if key == "media" || key == "tracklist" {
			var before, after rawField
			_ = json.Unmarshal(previous[key], &before)
			_ = json.Unmarshal(value, &after)
			if before.State == domain.Value && after.State == domain.Value {
				complete := false
				if key == "tracklist" {
					complete = string(next["tracklistCompleteness"]) == `"complete"`
				} else {
					var count domain.Field[int]
					var media []domain.Medium
					_ = json.Unmarshal(next["discTotal"], &count)
					_ = json.Unmarshal(after.Value, &media)
					complete = count.State == domain.Value && count.Value == len(media)
				}
				merged, err := mergeSnapshotEntries(before.Value, after.Value, key, complete)
				if err != nil {
					return nil, err
				}
				after.Value = merged
				value, err = json.Marshal(after)
				if err != nil {
					return nil, err
				}
			}
		}
		if key == "roles" {
			merged, err := mergeSnapshotJSON(previous[key], value)
			if err != nil {
				return nil, err
			}
			previous[key] = merged
			continue
		}
		var field rawField
		if json.Unmarshal(value, &field) == nil && (field.State == domain.Unknown || field.State == "") {
			if field.State == domain.Unknown {
				continue
			}
		}
		previous[key] = value
	}
	return json.Marshal(previous)
}

func mergeSnapshotEntries(before, after json.RawMessage, kind string, complete bool) (json.RawMessage, error) {
	var old, next []json.RawMessage
	if err := json.Unmarshal(before, &old); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(after, &next); err != nil {
		return nil, err
	}
	keyOf := func(value json.RawMessage) int {
		var pos struct{ Number, DiscNumber, TrackNumber int }
		_ = json.Unmarshal(value, &pos)
		if kind == "media" {
			return pos.Number
		}
		return pos.DiscNumber*100000 + pos.TrackNumber
	}
	previous := map[int]json.RawMessage{}
	for _, v := range old {
		previous[keyOf(v)] = v
	}
	out := map[int]json.RawMessage{}
	if !complete {
		for k, v := range previous {
			out[k] = v
		}
	}
	for _, v := range next {
		key := keyOf(v)
		prior := previous[key]
		if kind == "tracklist" {
			var oldEntry, newEntry domain.TracklistEntry
			_ = json.Unmarshal(prior, &oldEntry)
			_ = json.Unmarshal(v, &newEntry)
			if oldEntry.Track.State == domain.Value && newEntry.Track.State == domain.Value && oldEntry.Track.Value != newEntry.Track.Value {
				prior = nil
			}
		}
		merged, err := mergeSnapshotJSON(prior, v)
		if err != nil {
			return nil, err
		}
		out[key] = merged
	}
	var keys []int
	for key := range out {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	values := make([]json.RawMessage, 0, len(keys))
	for _, key := range keys {
		values = append(values, out[key])
	}
	return json.Marshal(values)
}
func saveSnapshotTx(tx *sql.Tx, kind string, id int64, snapshot any) ([]byte, error) {
	key := entityKey(kind)
	var old string
	if err := tx.QueryRow("SELECT body FROM "+kind+"_snapshot WHERE "+key+"=?", id).Scan(&old); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	merged, err := mergeSnapshotJSON([]byte(old), b)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec("INSERT INTO "+kind+"_snapshot("+key+",body,fetched_at) VALUES(?,?,?) ON CONFLICT("+key+") DO UPDATE SET body=excluded.body,fetched_at=excluded.fetched_at,revision=revision+1", id, string(merged), time.Now().UTC())
	if err == nil {
		_, err = tx.Exec("UPDATE "+kind+" SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UTC(), id)
	}
	return merged, err
}

func rebuildIdentifiersTx(tx *sql.Tx, id int64, snapshot domain.TrackSnapshot) error {
	if _, err := tx.Exec("DELETE FROM track_identifier WHERE track_id=?", id); err != nil {
		return err
	}
	for i, v := range snapshot.Identifiers.Value {
		if _, err := tx.Exec("INSERT INTO track_identifier(track_id,kind,value,position) VALUES(?,?,?,?)", id, v.Kind, v.Value, i); err != nil {
			return err
		}
	}
	return nil
}

func emptyCurrentTx(tx *sql.Tx, kind string, id int64) error {
	_, err := tx.Exec("INSERT INTO "+kind+"_current("+entityKey(kind)+") VALUES(?)", id)
	return err
}
