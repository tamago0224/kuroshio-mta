package util

import (
	"errors"
	"strings"
)

func NormalizePath(path string) (string, error) {
	s := strings.TrimSpace(path)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		s = strings.TrimPrefix(strings.TrimSuffix(s, ">"), "<")
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return "", errors.New("invalid mailbox path")
	}
	if s == "" {
		return "", nil
	}
	if !strings.Contains(s, "@") {
		return "", errors.New("mailbox missing @")
	}
	return strings.ToLower(s), nil
}

func DomainOf(addr string) (string, bool) {
	parts := strings.Split(addr, "@")
	if len(parts) != 2 || parts[1] == "" {
		return "", false
	}
	return strings.ToLower(parts[1]), true
}
