package mailauth

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

type spfMacroContext struct {
	sender      string
	domain      string
	helo        string
	remoteIP    net.IP
	explanation bool
}

func (m spfMacroContext) expand(spec string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(spec); i++ {
		ch := spec[i]
		if ch < 32 || ch > 126 || (ch == ' ' && !m.explanation) {
			return "", fmt.Errorf("invalid SPF macro literal")
		}
		if ch != '%' {
			out.WriteByte(ch)
			continue
		}
		i++
		if i == len(spec) {
			return "", fmt.Errorf("incomplete SPF macro escape")
		}
		switch spec[i] {
		case '%':
			out.WriteByte('%')
		case '_':
			out.WriteByte(' ')
		case '-':
			out.WriteString("%20")
		case '{':
			end := strings.IndexByte(spec[i+1:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated SPF macro")
			}
			token := spec[i+1 : i+1+end]
			value, err := m.value(token)
			if err != nil {
				return "", err
			}
			out.WriteString(value)
			i += end + 1
		default:
			return "", fmt.Errorf("invalid SPF macro escape")
		}
	}
	expanded := out.String()
	for _, ch := range expanded {
		if ch < 32 || ch > 126 {
			return "", fmt.Errorf("non-ASCII or control character in SPF macro result")
		}
	}
	return expanded, nil
}

func (m spfMacroContext) value(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("empty SPF macro")
	}
	letter := token[0]
	lower := letter
	if lower >= 'A' && lower <= 'Z' {
		lower += 'a' - 'A'
	}
	value, err := m.base(lower)
	if err != nil {
		return "", err
	}
	rest := token[1:]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	count := 0
	if digits > 0 {
		// Saturate large counts: all available parts are used without overflowing.
		positive := false
		for _, digit := range rest[:digits] {
			positive = positive || digit != '0'
			if count <= len(value) {
				count = count*10 + int(digit-'0')
			}
		}
		if !positive {
			return "", fmt.Errorf("zero SPF macro transformer")
		}
	}
	rest = rest[digits:]
	reverse := strings.HasPrefix(rest, "r") || strings.HasPrefix(rest, "R")
	if reverse {
		rest = rest[1:]
	}
	for _, ch := range rest {
		if !strings.ContainsRune(".-+,/_=", ch) {
			return "", fmt.Errorf("invalid SPF macro delimiter")
		}
	}
	if len(token) > 1 {
		delimiters := rest
		if delimiters == "" {
			delimiters = "."
		}
		// Split once per delimiter byte, retaining leading/trailing/adjacent empty parts.
		parts := []string{}
		start := 0
		for i, ch := range value {
			if strings.ContainsRune(delimiters, ch) {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
		parts = append(parts, value[start:])
		if reverse {
			slices.Reverse(parts)
		}
		if count > 0 && count < len(parts) {
			parts = parts[len(parts)-count:]
		}
		value = strings.Join(parts, ".")
	}
	if letter != lower {
		value = escapeSPFMacro(value)
	}
	return value, nil
}

func (m spfMacroContext) base(letter byte) (string, error) {
	sender := strings.TrimSpace(m.sender)
	if sender == "" {
		sender = "postmaster@" + m.helo
	}
	local, origin := sender, ""
	if at := strings.LastIndexByte(sender, '@'); at >= 0 {
		local, origin = sender[:at], sender[at+1:]
	}
	switch letter {
	case 's':
		return sender, nil
	case 'l':
		return local, nil
	case 'o':
		return strings.ToLower(origin), nil
	case 'd':
		return strings.ToLower(m.domain), nil
	case 'h':
		return strings.ToLower(m.helo), nil
	case 'p':
		return "unknown", nil
	case 'i':
		if ip := m.remoteIP.To4(); ip != nil {
			return ip.String(), nil
		}
		if ip := m.remoteIP.To16(); ip != nil {
			const hex = "0123456789abcdef"
			parts := make([]string, 0, 32)
			for _, b := range ip {
				parts = append(parts, string(hex[b>>4]), string(hex[b&15]))
			}
			return strings.Join(parts, "."), nil
		}
		return "", fmt.Errorf("missing SPF client IP")
	case 'v':
		if m.remoteIP.To4() != nil {
			return "in-addr", nil
		}
		return "ip6", nil
	case 'c', 'r', 't':
		if !m.explanation {
			return "", fmt.Errorf("SPF macro is only allowed in explanation text")
		}
		switch letter {
		case 'c':
			return m.remoteIP.String(), nil
		case 'r':
			return "unknown", nil
		default:
			return strconv.FormatInt(time.Now().Unix(), 10), nil
		}
	default:
		return "", fmt.Errorf("unknown SPF macro")
	}
}

func escapeSPFMacro(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		alpha := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		digit := ch >= '0' && ch <= '9'
		if alpha || digit || strings.ContainsRune("-._~", rune(ch)) {
			out.WriteByte(ch)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hex[ch>>4])
		out.WriteByte(hex[ch&15])
	}
	return out.String()
}

func (m spfMacroContext) domainSpec(spec string) (string, error) {
	expanded, err := m.expand(spec)
	if err != nil {
		return "", err
	}
	expanded = strings.TrimSuffix(expanded, ".")
	for len(expanded) > 253 {
		_, tail, ok := strings.Cut(expanded, ".")
		if !ok {
			return "", fmt.Errorf("SPF domain exceeds DNS length limit")
		}
		expanded = tail
	}
	if expanded == "" {
		return "", fmt.Errorf("empty SPF domain")
	}
	for _, label := range strings.Split(expanded, ".") {
		if len(label) == 0 || len(label) > 63 {
			return "", fmt.Errorf("invalid SPF domain label length")
		}
	}
	if strings.ContainsAny(expanded, " \t\r\n") {
		return "", fmt.Errorf("invalid SPF domain whitespace")
	}
	return expanded, nil
}

// SPF delimiters in %{...} belong to the macro, not to the enclosing term.
func spfDelimiterOutsideMacro(term string, delimiter byte) int {
	for i := 0; i < len(term); i++ {
		if term[i] == '%' && i+1 < len(term) {
			i++
			if term[i] == '{' {
				end := strings.IndexByte(term[i+1:], '}')
				if end < 0 {
					return -1
				}
				i += end + 1
			}
			continue
		}
		if term[i] == delimiter {
			return i
		}
	}
	return -1
}

func spfModifierSeparator(term string) int {
	separator := spfDelimiterOutsideMacro(term, '=')
	if separator >= 0 && strings.ContainsAny(term[:separator], ":/") {
		return -1
	}
	return separator
}

func (m spfMacroContext) explain(ctx context.Context, spec string) (string, bool) {
	domain, err := m.domainSpec(spec)
	if err != nil {
		return "", false
	}
	text, ok := lookupSPFExplanation(ctx, domain)
	if !ok {
		return "", false
	}
	m.explanation = true
	expanded, err := m.expand(text)
	if err != nil || len(expanded) > 512 {
		return "", false
	}
	return expanded, true
}
