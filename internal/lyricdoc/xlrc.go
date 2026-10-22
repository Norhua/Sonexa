package lyricdoc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// XLRC 0.1-draft plus the documented, lossless Sonexa extensions. Parsing
// never infers languages or voices; native online producers may label extras.
var languageTag = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*$`)
var xlrcHeader = regexp.MustCompile(`^\[([A-Za-z][A-Za-z0-9_-]*):([^\]]*)\]$`)

func rubyKana(s string) bool { return rubyKanaRunes([]rune(s)) }
func rubyKanaRunes(s []rune) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !(r >= 0x3040 && r <= 0x30ff || r >= 0x31f0 && r <= 0x31ff || r >= 0xff66 && r <= 0xff9f) {
			return false
		}
	}
	return true
}
func rubyHan(r rune) bool {
	return unicode.Is(unicode.Han, r) || strings.ContainsRune("々〆ヵヶ", r)
}

func parseRuby(s string, escaped bool) (string, []Ruby) {
	out := []rune{}
	var rubies []Ruby
	input := []rune(s)
	boundary, closeIndex := 0, -1
	for i := 0; i < len(input); i++ {
		r := input[i]
		if escaped && r == '\\' && i+1 < len(input) && strings.ContainsRune(`\[<`, input[i+1]) {
			i++
			out = append(out, input[i])
			boundary = len(out)
			continue
		}
		if r == '[' {
			if closeIndex <= i {
				closeIndex = i + 1
				for closeIndex < len(input) && input[closeIndex] != ']' {
					closeIndex++
				}
			}
			end := closeIndex
			start := len(out)
			for start > boundary && rubyHan(out[start-1]) {
				start--
			}
			if end < len(input) && start < len(out) && rubyKanaRunes(input[i+1:end]) {
				rubies = append(rubies, Ruby{Start: start, End: len(out), Reading: string(input[i+1 : end])})
				boundary = len(out)
				i = end
				continue
			}
		}
		out = append(out, r)
	}
	return string(out), rubies
}

func renderRuby(text string, rubies []Ruby, escaped bool) (string, error) {
	rs := []rune(text)
	previous := 0
	for _, r := range rubies {
		if r.Start < previous || r.End <= r.Start || r.End > len(rs) || !rubyKana(r.Reading) || r.Start > previous && rubyHan(rs[r.Start-1]) {
			return "", ErrFormat
		}
		for _, base := range rs[r.Start:r.End] {
			if !rubyHan(base) {
				return "", ErrFormat
			}
		}
		previous = r.End
	}
	var out strings.Builder
	j := 0
	for i, r := range rs {
		if escaped && strings.ContainsRune(`\[<`, r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
		if j < len(rubies) && i+1 == rubies[j].End {
			out.WriteString("[" + rubies[j].Reading + "]")
			j++
		}
	}
	return out.String(), nil
}

func richLine(l Line, escaped bool) (Line, error) {
	if len(l.Parts) == 0 {
		l.Text, l.Ruby = parseRuby(l.Text, escaped)
		return l, nil
	}
	l.Ruby = nil
	var text strings.Builder
	shift := 0
	for i := range l.Parts {
		p := &l.Parts[i]
		p.Text, p.Ruby = parseRuby(p.Text, escaped)
		for _, r := range p.Ruby {
			r.Start += shift
			r.End += shift
			l.Ruby = append(l.Ruby, r)
		}
		text.WriteString(p.Text)
		shift += len([]rune(p.Text))
	}
	l.Text = text.String()
	return l, nil
}

func isXLRCText(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimPrefix(line, "\ufeff")
		if strings.HasPrefix(line, "[xlrc:") || strings.HasPrefix(line, "[>") || strings.HasPrefix(line, "[x-sonexa:") {
			return true
		}
	}
	return false
}

// DecodeInline interprets only explicit XLRC voice/ruby syntax in a native
// adapter's already decoded line. It does not invent singer or language data.
func DecodeInline(l Line) (Line, error) {
	if strings.HasPrefix(l.Text, "[v:") {
		end := strings.IndexByte(l.Text, ']')
		if end < 4 {
			return l, ErrFormat
		}
		label := l.Text[:end+1]
		l.Voice, l.Text = l.Text[3:end], l.Text[end+1:]
		if len(l.Parts) > 0 {
			if !strings.HasPrefix(l.Parts[0].Text, label) {
				return l, ErrFormat
			}
			l.Parts[0].Text = strings.TrimPrefix(l.Parts[0].Text, label)
		}
	}
	return richLine(l, false)
}

func lineLanguage(l Line, key string) string {
	if l.Language != "" {
		return l.Language
	}
	if key == "additional" {
		return "und-x-additional"
	}
	if key == "romanization" {
		return "und-Latn"
	}
	return "und"
}
func languageRole(lang string) string {
	lower := strings.ToLower(lang)
	if strings.Contains(lower, "-x-additional") {
		return "additional"
	}
	for _, part := range strings.Split(lower, "-") {
		if part == "latn" {
			return "romanization"
		}
	}
	return "translation"
}

// XLRC escapes only syntax-significant characters in literal text. Timing
// ends remain explicit; no adjacent start is invented as a previous end.
func serializeXLRCLine(l Line, subordinate bool) (string, error) {
	if _, err := serializeLine(l); err != nil {
		return "", err
	}
	var out strings.Builder
	if !subordinate {
		if l.StartMS == nil {
			out.WriteString("[x-text]")
		} else {
			out.WriteString("[" + clock(*l.StartMS) + "]")
		}
		if l.Voice != "" {
			out.WriteString("[v:" + l.Voice + "]")
		}
	}
	if l.EndMS != nil {
		out.WriteString(fmt.Sprintf("[x-end:%d]", *l.EndMS))
	}
	if len(l.Parts) == 0 {
		s, err := renderRuby(l.Text, l.Ruby, true)
		if err != nil {
			return "", err
		}
		out.WriteString(s)
	} else {
		for _, p := range l.Parts {
			if p.StartMS != nil {
				out.WriteString("<" + clock(*p.StartMS) + ">")
			}
			s, err := renderRuby(p.Text, p.Ruby, true)
			if err != nil {
				return "", err
			}
			out.WriteString(s)
			if p.EndMS != nil {
				out.WriteString("<" + clock(*p.EndMS) + ">")
			}
		}
	}
	return out.String(), nil
}

func previewLine(l Line, channel string) PreviewRow {
	return PreviewRow{TimeMS: l.StartMS, Text: l.Text, Channel: channel, Voice: l.Voice, Ruby: l.Ruby, Language: previewLanguage(l, channel)}
}

func previewLanguage(l Line, key string) string {
	if key == "original" {
		return l.Language
	}
	return lineLanguage(l, key)
}

func parentResolver(original *Track) func(Line) int {
	ids := map[string]int{}
	times := map[int64]int{}
	voices := map[string]int{}
	put := func(m map[string]int, key string, i int) {
		if _, ok := m[key]; ok {
			m[key] = -1
		} else {
			m[key] = i
		}
	}
	for i, l := range original.Lines {
		if l.ID != "" {
			put(ids, l.ID, i)
		}
		if l.StartMS == nil || strings.TrimSpace(l.Text) == "" {
			continue
		}
		if _, ok := times[*l.StartMS]; ok {
			times[*l.StartMS] = -1
		} else {
			times[*l.StartMS] = i
		}
		put(voices, fmt.Sprintf("%d\x00%s", *l.StartMS, l.Voice), i)
	}
	return func(l Line) int {
		if l.Independent {
			return -1
		}
		if l.ParentID != "" {
			if i, ok := ids[l.ParentID]; ok {
				return i
			}
			return -1
		}
		if l.StartMS == nil {
			return -1
		}
		if l.Voice != "" {
			if i, ok := voices[fmt.Sprintf("%d\x00%s", *l.StartMS, l.Voice)]; ok {
				return i
			}
			return -1
		}
		if i, ok := times[*l.StartMS]; ok {
			return i
		}
		return -1
	}
}

// SerializeXLRC serializes completed channels. It never reparses, classifies
// source data, or alters a document. Build invokes it before sealing.
func SerializeXLRC(d *Document) (string, error) {
	if d == nil || d.Original == nil || d.Original.Track == nil {
		return "", ErrNoLyrics
	}
	if err := validateTrackSize(d.Original.Track); err != nil {
		return "", err
	}
	for _, ch := range append([]*Channel{d.Translation, d.Romanization}, d.Additional...) {
		if ch != nil && ch.Track != nil {
			if err := validateTrackSize(ch.Track); err != nil {
				return "", err
			}
		}
	}
	var out strings.Builder
	add := func(s string) error {
		if out.Len()+len(s)+1 > MaxTextBytes {
			return ErrTooLarge
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s)
		return nil
	}
	if err := add("[xlrc:0.1]\n[x-sonexa:1]"); err != nil {
		return "", err
	}
	for _, tag := range d.Original.Track.Metadata {
		if strings.HasPrefix(tag, "[xlrc:") || strings.HasPrefix(tag, "[x-") {
			continue
		}
		if err := add(tag); err != nil {
			return "", err
		}
	}
	hasOffset := false
	for _, tag := range d.Original.Track.Metadata {
		hasOffset = hasOffset || isOffset(tag)
	}
	if !hasOffset && d.OffsetMS != 0 {
		if err := add(fmt.Sprintf("[offset:%d]", d.OffsetMS)); err != nil {
			return "", err
		}
	}
	type extra struct {
		line Line
		key  string
	}
	attached := make(map[int][]extra)
	independent := []extra{}
	entries := []channelTrack{{"translation", d.Translation}, {"romanization", d.Romanization}}
	for _, ch := range d.Additional {
		entries = append(entries, channelTrack{"additional", ch})
	}
	resolve := parentResolver(d.Original.Track)
	for _, entry := range entries {
		if entry.channel == nil || entry.channel.Track == nil {
			continue
		}
		key := entry.key
		if entry.channel.Status != "available" {
			key = "additional"
		}
		for _, l := range entry.channel.Track.Lines {
			e := extra{l, key}
			target := -1
			if (entry.channel.Status == "available" && key != "additional") || l.ParentID != "" {
				target = resolve(l)
			}
			if target >= 0 {
				attached[target] = append(attached[target], e)
			} else {
				independent = append(independent, e)
			}
		}
	}
	for i, l := range d.Original.Track.Lines {
		s, err := serializeXLRCLine(l, false)
		if err != nil {
			return "", err
		}
		if err = add(s); err != nil {
			return "", err
		}
		for _, e := range attached[i] {
			lang := lineLanguage(e.line, e.key)
			// A language code alone cannot always express the producer's role.
			prefix := ""
			if languageRole(lang) != e.key {
				prefix = "[x-role:" + e.key + "]"
			}
			s, err := serializeXLRCLine(e.line, true)
			if err != nil {
				return "", err
			}
			if err = add("[>" + lang + "]" + prefix + s); err != nil {
				return "", err
			}
		}
	}
	for _, e := range independent {
		if err := add("[x-channel:" + e.key + ":" + lineLanguage(e.line, e.key) + "]"); err != nil {
			return "", err
		}
		s, err := serializeXLRCLine(e.line, false)
		if err != nil {
			return "", err
		}
		if err = add(s); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

// ParseXLRC is deliberately strict at the adoption boundary: malformed
// controls fail instead of silently discarding a user's lyrics. Unknown
// language tags are kept without language detection.
func ParseXLRC(text string) (*Document, error) {
	text, err := normalizeText(text)
	if err != nil {
		return nil, err
	}
	escaped := false
	for _, line := range strings.Split(text, "\n") {
		if line == "[x-sonexa:1]" {
			escaped = true
		}
	}
	original := &Channel{Status: "available", Origin: "independent", Track: &Track{}}
	translation := AbsentChannel()
	romanization := AbsentChannel()
	additional := &Channel{Status: "unknown", Origin: "unknown"}
	channel := original
	role, lang := "original", ""
	parents := []Line{}
	used := 0
	getChannel := func(key string) *Channel {
		switch key {
		case "original":
			return original
		case "translation":
			return translation
		case "romanization":
			return romanization
		case "additional":
			return additional
		}
		return nil
	}
	appendLine := func(ch *Channel, l Line) error {
		used += lineBytes(l)
		if used > MaxProjectionBytes {
			return ErrTooLarge
		}
		if ch.Track == nil {
			ch.Track = &Track{}
		}
		if ch != additional {
			ch.Status = "available"
			ch.Origin = "independent"
		}
		ch.Track.Lines = append(ch.Track.Lines, l)
		return nil
	}
	for number, raw := range strings.Split(text, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if strings.HasPrefix(raw, "[x-sonexa:") && raw != "[x-sonexa:1]" {
			return nil, ErrFormat
		}
		if raw == "[x-sonexa:1]" {
			continue
		}
		if strings.HasPrefix(raw, "[x-channel:") {
			if !escaped {
				return nil, ErrFormat
			}
			end := strings.IndexByte(raw, ']')
			if end < 11 || end != len(raw)-1 {
				return nil, ErrFormat
			}
			fields := strings.Split(raw[11:end], ":")
			if len(fields) != 2 || !languageTag.MatchString(fields[1]) {
				return nil, ErrFormat
			}
			role, lang = fields[0], fields[1]
			channel = getChannel(role)
			if channel == nil {
				return nil, ErrFormat
			}
			parents = nil
			continue
		}
		if strings.HasPrefix(raw, "[>") {
			end := strings.IndexByte(raw, ']')
			if end < 3 || !languageTag.MatchString(raw[2:end]) || len(parents) == 0 {
				return nil, fmt.Errorf("%w：孤立或无效 XLRC 附加行", ErrFormat)
			}
			language := raw[2:end]
			key := languageRole(language)
			body := raw[end+1:]
			if strings.HasPrefix(body, "[x-role:") && escaped {
				e := strings.IndexByte(body, ']')
				if e < 0 {
					return nil, ErrFormat
				}
				key = body[8:e]
				body = body[e+1:]
			}
			ch := getChannel(key)
			if ch == nil || ch == original {
				return nil, ErrFormat
			}
			// Per XLRC, a subordinate voice is ignored and inherited.
			if strings.HasPrefix(body, "[v:") {
				e := strings.IndexByte(body, ']')
				if e < 0 {
					return nil, ErrFormat
				}
				body = body[e+1:]
			}
			for _, parent := range parents {
				l, err := parseXLRCBody(body, parent.StartMS, escaped)
				if err != nil {
					return nil, err
				}
				l.Language, l.Voice, l.ParentID = language, parent.Voice, parent.ID
				if err = appendLine(ch, l); err != nil {
					return nil, err
				}
			}
			continue
		}
		if m := xlrcHeader.FindStringSubmatch(raw); m != nil {
			if strings.HasPrefix(m[1], "x-") {
				return nil, ErrFormat
			}
			if m[1] == "xlrc" && m[2] != "0.1" {
				return nil, fmt.Errorf("%w：不支持的 XLRC 版本", ErrFormat)
			}
			if m[1] == "lang" {
				if !languageTag.MatchString(m[2]) {
					return nil, ErrFormat
				}
				lang = m[2]
			}
			original.Track.Metadata = append(original.Track.Metadata, raw)
			continue
		}
		body := raw
		starts := []*int64{}
		for {
			m := lineTime.FindStringSubmatch(body)
			if m == nil {
				break
			}
			n, err := timestamp(m)
			if err != nil {
				return nil, err
			}
			starts = append(starts, ms(n))
			body = body[len(m[0]):]
		}
		if len(starts) == 0 {
			if !strings.HasPrefix(body, "[x-text]") {
				return nil, fmt.Errorf("%w：XLRC 第 %d 行缺少时间或 x-text", ErrFormat, number+1)
			}
			body = strings.TrimPrefix(body, "[x-text]")
			starts = append(starts, nil)
		}
		parents = nil
		for _, start := range starts {
			l, err := parseXLRCBody(body, start, escaped)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(l.Voice) == "" && l.Voice != "" {
				return nil, ErrFormat
			}
			if l.StartMS == nil && !escaped {
				return nil, ErrFormat
			}
			l.Language = lang
			l.Independent = role != "original"
			if role == "original" {
				l.ID = fmt.Sprintf("line-%d", len(original.Track.Lines))
				parents = append(parents, l)
			}
			if err = appendLine(channel, l); err != nil {
				return nil, err
			}
		}
	}
	// Use the ordinary metadata validator for consistent offset diagnostics.
	meta, _, metaErr := parseTrack(strings.Join(original.Track.Metadata, "\n") + "\nmetadata sentinel")
	if metaErr != nil {
		return nil, metaErr
	}
	for _, ch := range []*Channel{original, translation, romanization, additional} {
		if ch.Track != nil {
			ch.Track.OffsetMS = meta.OffsetMS
			ch.Track.Diagnostics = cloneDiagnostics(meta.Diagnostics)
		}
	}
	extras := []*Channel{}
	if additional.Track != nil {
		extras = append(extras, additional)
	}
	d, err := buildDocument(original, translation, romanization, extras, "", nil)
	if err != nil {
		return nil, err
	}
	d.Text, d.Format = text, "xlrc"
	if err = seal(d); err != nil {
		return nil, err
	}
	return d, nil
}

func parseXLRCBody(body string, start *int64, escaped bool) (Line, error) {
	l := Line{StartMS: start}
	if strings.HasPrefix(body, "[v:") {
		end := strings.IndexByte(body, ']')
		if end < 0 || end == 3 {
			return l, ErrFormat
		}
		l.Voice = body[3:end]
		body = body[end+1:]
	}
	if strings.HasPrefix(body, "[x-end:") && escaped {
		end := strings.IndexByte(body, ']')
		if end < 0 {
			return l, ErrFormat
		}
		n, err := strconv.ParseInt(body[7:end], 10, 64)
		if err != nil {
			return l, ErrFormat
		}
		l.EndMS = ms(n)
		body = body[end+1:]
	}
	var err error
	l.Parts, l.Text, err = parseWordsMode(body, escaped)
	if err != nil {
		return l, err
	}
	l, err = richLine(l, escaped)
	if err != nil {
		return l, err
	}
	if _, err = serializeLine(l); err != nil {
		return l, err
	}
	return l, nil
}
