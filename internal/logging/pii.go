package logging

import (
	"fmt"
	"net"
	"strings"
)

func MaskEmail(v string) string {
	s := strings.TrimSpace(strings.ToLower(v))
	if s == "" {
		return ""
	}
	at := strings.LastIndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return "***"
	}
	local := s[:at]
	domain := s[at+1:]
	if len(local) <= 2 {
		return "***@" + domain
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + domain
}

func MaskIP(v string) string {
	ip := net.ParseIP(strings.TrimSpace(v))
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%d.%d.x.x", ip4[0], ip4[1])
	}
	s := ip.String()
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return "x::"
	}
	return parts[0] + ":x::"
}
