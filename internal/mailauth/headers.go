package mailauth

import (
	"errors"
	"strings"
)

type Header struct {
	Name  string
	Value string
	Raw   string
}

func SplitMessage(data []byte) (headerPart string, bodyPart string, err error) {
	s := string(data)
	if i := strings.Index(s, "\r\n\r\n"); i >= 0 {
		return s[:i], s[i+4:], nil
	}
	if i := strings.Index(s, "\n\n"); i >= 0 {
		return s[:i], s[i+2:], nil
	}
	return "", "", errors.New("missing header/body separator")
}

func ParseHeaders(headerPart string) ([]Header, error) {
	lines := splitLines(headerPart)
	if len(lines) == 0 {
		return nil, errors.New("empty headers")
	}
	var headers []Header
	var curName string
	var curVal strings.Builder
	flush := func() {
		if curName == "" {
			return
		}
		v := curVal.String()
		headers = append(headers, Header{Name: curName, Value: unfold(v), Raw: v})
		curName = ""
		curVal.Reset()
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if curName == "" {
				continue
			}
			curVal.WriteByte('\n')
			curVal.WriteString(line)
			continue
		}
		flush()
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		curName = strings.TrimSpace(line[:idx])
		curVal.WriteString(strings.TrimSpace(line[idx+1:]))
	}
	flush()
	return headers, nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func unfold(v string) string {
	x := strings.ReplaceAll(v, "\r\n", " ")
	x = strings.ReplaceAll(x, "\n", " ")
	return strings.Join(strings.Fields(x), " ")
}

func HeaderValues(headers []Header, name string) []string {
	name = strings.ToLower(name)
	var out []string
	for _, h := range headers {
		if strings.ToLower(h.Name) == name {
			out = append(out, h.Value)
		}
	}
	return out
}

func FirstHeader(headers []Header, name string) (string, bool) {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}
