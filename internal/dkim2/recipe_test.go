package dkim2

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestReconstructPreviousMessage(t *testing.T) {
	recipeJSON := `{"h":{"subject":[{"d":[" previous"]}]},"b":[{"d":["old body"]}]}`
	encoded := base64.StdEncoding.EncodeToString([]byte(recipeJSON))
	headers, body, err := reconstructPrevious(
		[]field{{name: "From", value: "sender@example.test"}, {name: "Subject", value: "current"}},
		[]byte("current body\r\n"),
		encoded,
		DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old body\r\n" {
		t.Fatalf("body=%q", body)
	}
	if got := string(canonicalMessageHeaders(headers)); !strings.Contains(got, "subject:previous\r\n") {
		t.Fatalf("headers=%q", got)
	}
}

func TestReconstructRecipeCopyRanges(t *testing.T) {
	recipeJSON := `{"h":{"received-test":[{"c":[1,2]}]},"b":[{"c":[2,3]}]}`
	encoded := base64.StdEncoding.EncodeToString([]byte(recipeJSON))
	headers, body, err := reconstructPrevious(
		[]field{{name: "Received-Test", value: "top"}, {name: "Received-Test", value: "bottom"}},
		[]byte("one\r\ntwo\r\nthree\r\n"),
		encoded,
		DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "two\r\nthree\r\n" || len(headers) != 2 {
		t.Fatalf("headers=%+v body=%q", headers, body)
	}
}

func TestReconstructRejectsInvalidRecipes(t *testing.T) {
	tests := []struct {
		name   string
		recipe string
		reason string
	}{
		{name: "unknown root member", recipe: `{"x":[]}`, reason: "unknown recipe member"},
		{name: "body unavailable", recipe: `{"b":null}`, reason: "unrecoverable"},
		{name: "invalid range", recipe: `{"b":[{"c":[2,1]}]}`, reason: "copy range"},
		{name: "newline injection", recipe: `{"b":[{"d":["bad\nline"]}]}`, reason: "CR or LF"},
		{name: "multiple operations", recipe: `{"b":[{"c":[1,1],"d":["line"]}]}`, reason: "exactly one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString([]byte(test.recipe))
			_, _, err := reconstructPrevious(nil, []byte("line\r\n"), encoded, DefaultLimits())
			if err == nil || !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
