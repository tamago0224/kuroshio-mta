package dkim

import (
	"bytes"
	"errors"
	"net/mail"
	"strings"
)

// FromAddress requires exactly one author and rejects multiple From fields.
func FromAddress(raw []byte) (string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("invalid signing author")
	}
	values := msg.Header["From"]
	if len(values) != 1 {
		return "", errors.New("signing requires one From field")
	}
	addresses, err := mail.ParseAddressList(values[0])
	if err != nil || len(addresses) != 1 {
		return "", errors.New("signing requires one author")
	}
	address := strings.ToLower(addresses[0].Address)
	at := strings.LastIndexByte(address, '@')
	if at <= 0 || !validSigningName(address[at+1:]) {
		return "", errors.New("invalid signing author")
	}
	return address, nil
}
