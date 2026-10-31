package importer

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"golang.org/x/text/unicode/norm"
)

var durationValue = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]{1,3})?)\s*(ms|milliseconds?|毫秒|s|sec(?:onds?)?|秒)?$`)
var durationClock = regexp.MustCompile(`^([0-9]+:)?[0-9]+:[0-9]{2}(?:\.[0-9]{1,3})?$`)
var positionValue = regexp.MustCompile(`^([0-9]+)(?:\s*/\s*([0-9]+))?$`)

// ParseDurationEvidence never guesses units from magnitude. A clock, a cell
// suffix, or an explicit column unit is required for identity evidence.
func ParseDurationEvidence(raw, header string) matching.NumberInput {
	out := matching.MissingNumber("ms", "duration")
	out.Raw = raw
	s := strings.TrimSpace(norm.NFKC.String(raw))
	if s == "" {
		return out
	}
	out.State, out.Error = matching.ValueInvalid, "无法确定时长格式或单位"
	var ms, precision int64
	if durationClock.MatchString(s) {
		parts := strings.Split(s, ":")
		seconds, p, ok := decimalMilliseconds(parts[len(parts)-1])
		if !ok || seconds >= 60000 {
			return out
		}
		var minutes int64
		for i, part := range parts[:len(parts)-1] {
			n, err := strconv.ParseInt(part, 10, 32)
			if err != nil || (i > 0 && n >= 60) {
				return out
			}
			minutes = minutes*60 + n
		}
		ms, precision = minutes*60000+seconds, p
	} else if m := durationValue.FindStringSubmatch(s); m != nil {
		unit := durationUnit(m[2])
		if m[2] == "" {
			unit = durationHeaderUnit(header)
		}
		switch unit {
		case "s":
			var ok bool
			ms, precision, ok = decimalMilliseconds(m[1])
			if !ok {
				return out
			}
		case "ms":
			var err error
			ms, err = strconv.ParseInt(m[1], 10, 53)
			if err != nil {
				return out
			}
			precision = 1
		default:
			return out
		}
	} else {
		return out
	}
	if ms <= 0 || ms > 1<<53-1 {
		return out
	}
	out.Value, out.Precision, out.State, out.Error = ms, precision, matching.ValueKnown, ""
	return out
}

func decimalMilliseconds(s string) (int64, int64, bool) {
	whole, fraction, _ := strings.Cut(s, ".")
	seconds, err := strconv.ParseInt(whole, 10, 43)
	if err != nil {
		return 0, 0, false
	}
	precision := int64(1000)
	for range fraction {
		precision /= 10
	}
	var millis int64
	if fraction != "" {
		millis, err = strconv.ParseInt(fraction+strings.Repeat("0", 3-len(fraction)), 10, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	return seconds*1000 + millis, precision, true
}

func durationUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "ms", "millisecond", "milliseconds", "毫秒":
		return "ms"
	case "s", "sec", "second", "seconds", "秒":
		return "s"
	default:
		return ""
	}
}

func durationHeaderUnit(header string) string {
	h := strings.ToLower(norm.NFKC.String(strings.TrimSpace(header)))
	if loc := unitSuffix.FindStringIndex(h); loc != nil {
		return durationUnit(strings.Trim(h[loc[0]:loc[1]], "()"))
	}
	for _, sep := range []string{"_", " "} {
		if i := strings.LastIndex(h, sep); i >= 0 {
			if unit := durationUnit(h[i+len(sep):]); unit != "" {
				return unit
			}
		}
	}
	return ""
}

func ParsePositionEvidence(raw, semantic string) matching.NumberInput {
	out := matching.MissingNumber("number", semantic)
	out.Raw = raw
	s := strings.TrimSpace(norm.NFKC.String(raw))
	if s == "" {
		return out
	}
	out.State, out.Error = matching.ValueInvalid, "位置必须为正整数或有效的 n/总数"
	m := positionValue.FindStringSubmatch(s)
	if m == nil {
		return out
	}
	n, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil || n <= 0 {
		return out
	}
	if m[2] != "" {
		total, err := strconv.ParseInt(m[2], 10, 32)
		if err != nil || total < n {
			return out
		}
	}
	out.State, out.Value, out.Precision, out.Error = matching.ValueKnown, n, 1, ""
	return out
}

func ParseYearEvidence(raw string) matching.NumberInput {
	out := matching.MissingNumber("year", "unspecified_year")
	out.Raw = raw
	s := strings.TrimSpace(norm.NFKC.String(raw))
	if s == "" {
		return out
	}
	out.State, out.Error = matching.ValueInvalid, "无法解析年份"
	for _, format := range []string{"2006", "2006-01", "2006-01-02", "2006/01/02", "2006年1月2日", "2006年"} {
		if date, err := time.Parse(format, s); err == nil && date.Year() >= 1000 && date.Year() <= 3000 {
			out.State, out.Value, out.Precision, out.Error = matching.ValueKnown, int64(date.Year()), 1, ""
			break
		}
	}
	return out
}

func numberPointer(n matching.NumberInput) *int {
	if n.State != matching.ValueKnown {
		return nil
	}
	v := int(n.Value)
	return &v
}
