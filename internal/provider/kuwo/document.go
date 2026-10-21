package kuwo

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
)

var metadataLine = regexp.MustCompile(`^\[[a-zA-Z]+:[^\]]*\]$`)

const translationRuleVersion = "kuwo-embedded-v1"

func ms(n int64) *int64 { return &n }

func plainRows(rows []lyricRow, reason string) *lyricdoc.Input {
	t := &lyricdoc.Track{}
	for _, row := range rows {
		t.Lines = append(t.Lines, lyricdoc.Line{Text: row.text})
	}
	code := "invalid_timeline"
	if strings.HasPrefix(reason, "offset") {
		code = "invalid_offset"
	}
	return &lyricdoc.Input{Original: &lyricdoc.Channel{Status: "available", Track: t, Diagnostics: []lyricdoc.Diagnostic{{Code: code, Message: reason}}}, WordTiming: "unsupported", Warnings: []string{reason}}
}

// prepareRows interprets only this endpoint's row grammar, before the public
// model sees it. On any invalid time/offset all text stays in source order.
func prepareRows(rows []lyricRow, validTimes bool, c lyrics.Candidate) (*lyricdoc.Input, error) {
	var offset *int64
	invalidOffset := false
	content := false
	for i := range rows {
		text := strings.TrimSpace(rows[i].text)
		rows[i].metadata = metadataLine.MatchString(text)
		if text != "" && !rows[i].metadata {
			content = true
		}
		if !rows[i].metadata || !strings.HasPrefix(strings.ToLower(text), "[offset:") {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(text[8:len(text)-1]), 10, 64)
		if err != nil || n < -lyricdoc.MaxTimeMS || n > lyricdoc.MaxTimeMS || offset != nil && *offset != n {
			invalidOffset = true
		} else {
			offset = ms(n)
		}
	}
	if !content {
		return nil, lyricdoc.ErrNoLyrics
	}
	if !validTimes || invalidOffset {
		reason := "时间轴无效，按来源原序保留全部正文为普通文本"
		if invalidOffset {
			reason = "offset 无效或互相矛盾，禁用识别并按来源原序保留普通文本"
		}
		return plainRows(rows, reason), nil
	}
	inferred := wholeAlternation(rows, c)
	inferred = append(inferred, nextTimeAssociations(rows, inferred)...)
	selected := map[int]inferredLine{}
	for _, line := range inferred {
		selected[line.row] = line
	}
	original, translation := &lyricdoc.Track{}, &lyricdoc.Track{}
	if offset != nil {
		original.OffsetMS, translation.OffsetMS = *offset, *offset
	}
	for i, row := range rows {
		if row.metadata {
			original.Metadata = append(original.Metadata, row.text)
			continue
		}
		line := lyricdoc.Line{StartMS: ms(row.timeMS), Text: row.text}
		if match, ok := selected[i]; ok {
			line.StartMS = ms(rows[match.original].timeMS)
			translation.Lines = append(translation.Lines, line)
		} else {
			original.Lines = append(original.Lines, line)
		}
	}
	// The endpoint has line-only data. Final normalization follows detection;
	// no source row is removed before language statistics or association.
	normalize := func(t *lyricdoc.Track) {
		sort.SliceStable(t.Lines, func(i, j int) bool { return *t.Lines[i].StartMS < *t.Lines[j].StartMS })
		seen := map[string]bool{}
		out := t.Lines[:0]
		for _, line := range t.Lines {
			key := fmt.Sprintf("%d\x00%s", *line.StartMS, line.Text)
			if !seen[key] {
				out = append(out, line)
				seen[key] = true
			}
		}
		t.Lines = out
	}
	normalize(original)
	normalize(translation)
	input := &lyricdoc.Input{Original: &lyricdoc.Channel{Status: "available", Track: original}, WordTiming: "unsupported"}
	if len(inferred) > 0 {
		ch := &lyricdoc.Channel{Status: "available", Origin: "embedded", Track: translation}
		for _, layer := range []int{1, 2} {
			basis := &lyricdoc.Inference{RuleVersion: translationRuleVersion, Layer: layer}
			for _, match := range inferred {
				if match.layer != layer {
					continue
				}
				source, owner := rows[match.row], rows[match.original]
				basis.Associations = append(basis.Associations, lyricdoc.LineAssociation{SourcePosition: source.position, OriginalPosition: owner.position, SourceTimeMS: source.timeMS, AssociatedTimeMS: owner.timeMS})
			}
			basis.Count = len(basis.Associations)
			if basis.Count > 0 {
				ch.Diagnostics = append(ch.Diagnostics, lyricdoc.Diagnostic{Code: "heuristic_translation", Message: "启发式识别：仅重分类来源已有文字，可能误判或漏判", Inference: basis})
			}
		}
		input.Translation = ch
	}
	return input, nil
}
