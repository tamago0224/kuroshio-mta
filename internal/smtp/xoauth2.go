package smtp

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

const maxAuthLineBytes = 12288

func (s *Server) authMechanisms(encrypted bool) []string {
	if !s.submission || s.authBackend == nil {
		return nil
	}
	methods := []string{"PLAIN", "LOGIN"}
	if b, ok := s.authBackend.(userauth.MechanismBackend); ok {
		methods = b.AuthMechanisms()
	}
	var result []string
	for _, method := range methods {
		if method != "XOAUTH2" || encrypted {
			result = append(result, method)
		}
	}
	return result
}

var errSASLFraming = errors.New("SASL response incomplete or too long")

// readBoundedSMTPLine caps allocation before processing credentials or commands.
func readBoundedSMTPLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > maxAuthLineBytes {
			return "", errSASLFraming
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", errSASLFraming
		}
		return string(line), nil
	}
}

func readSASLLine(r *bufio.Reader) (string, error) {
	line, err := readBoundedSMTPLine(r)
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}

func decodeXOAuth2(payload string) (string, string, error) {
	invalid := errors.New("invalid XOAUTH2 payload")
	if len(payload) > maxAuthLineBytes || strings.ContainsAny(payload, "\r\n") {
		return "", "", invalid
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		return "", "", invalid
	}
	fields := strings.Split(string(raw), "\x01")
	if len(fields) != 4 || fields[2] != "" || fields[3] != "" || !strings.HasPrefix(fields[0], "user=") || !strings.HasPrefix(fields[1], "auth=Bearer ") {
		return "", "", invalid
	}
	user, token := strings.TrimPrefix(fields[0], "user="), strings.TrimPrefix(fields[1], "auth=Bearer ")
	if user == "" || token == "" || len(token) > 8192 {
		return "", "", invalid
	}
	for _, v := range []string{user, token} {
		for _, c := range v {
			if c <= 32 || c >= 127 {
				return "", "", invalid
			}
		}
	}
	return user, token, nil
}

func (s *Server) handleXOAuth2(ctx context.Context, r *bufio.Reader, w *bufio.Writer, parts []string) (userauth.AuthResult, error) {
	if len(parts) > 2 {
		return userauth.AuthResult{}, errors.New("invalid XOAUTH2 command")
	}
	var payload string
	if len(parts) == 2 {
		payload = parts[1]
	} else {
		if writeLine(w, "334 ") != nil {
			return userauth.AuthResult{}, io.ErrClosedPipe
		}
		var err error
		payload, err = readSASLLine(r)
		if err != nil {
			return userauth.AuthResult{}, err
		}
	}
	if payload == "*" {
		return userauth.AuthResult{}, errors.New("authentication canceled")
	}
	user, token, err := decodeXOAuth2(payload)
	if err != nil {
		return userauth.AuthResult{}, err
	}
	backend, ok := s.authBackend.(userauth.BearerBackend)
	if !ok {
		return userauth.AuthResult{}, errors.New("unsupported auth mechanism")
	}
	result := backend.AuthenticateBearerToken(ctx, user, token)
	if result.Success {
		return result, nil
	}
	challenge := base64.StdEncoding.EncodeToString([]byte(`{"status":"401","schemes":"bearer"}`))
	if writeLine(w, "334 "+challenge) != nil {
		return userauth.AuthResult{}, io.ErrClosedPipe
	}
	response, err := readSASLLine(r)
	if err != nil {
		return userauth.AuthResult{}, err
	}
	if response != "" && response != "*" {
		return userauth.AuthResult{}, errors.New("invalid XOAUTH2 error response")
	}
	return result, nil
}
