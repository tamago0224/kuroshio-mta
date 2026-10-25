package dkim2

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type recipe struct {
	Headers map[string][]recipeStep `json:"h"`
	Body    json.RawMessage         `json:"b"`
}

type recipeStep struct {
	Copy   []int    `json:"c,omitempty"`
	Data   []string `json:"d,omitempty"`
	Binary []string `json:"u,omitempty"`
}

func decodeRecipe(encoded string, limits Limits) (recipe, bool, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil {
		return recipe{}, false, errors.New("invalid recipe base64")
	}
	if len(decoded) > limits.MaxRecipeBytes {
		return recipe{}, false, errors.New("recipe size limit exceeded")
	}
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	if err := decoder.Decode(&raw); err != nil {
		return recipe{}, false, errors.New("invalid recipe JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return recipe{}, false, errors.New("invalid recipe JSON")
	}
	if len(raw) == 0 || len(raw) > 2 {
		return recipe{}, false, errors.New("recipe requires only h or b")
	}
	for key := range raw {
		if key != "h" && key != "b" {
			return recipe{}, false, fmt.Errorf("unknown recipe member %q", key)
		}
	}
	var result recipe
	if value, ok := raw["h"]; ok {
		var headerRecipes map[string]json.RawMessage
		if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &headerRecipes) != nil {
			return recipe{}, false, errors.New("invalid header recipe")
		}
		result.Headers = make(map[string][]recipeStep, len(headerRecipes))
		for name, rawSteps := range headerRecipes {
			steps, err := decodeRecipeSteps(rawSteps)
			if err != nil {
				return recipe{}, false, fmt.Errorf("header recipe %s: %w", name, err)
			}
			result.Headers[name] = steps
		}
	}
	bodyUnrecoverable := false
	if value, ok := raw["b"]; ok {
		result.Body = value
		bodyUnrecoverable = bytes.Equal(value, []byte("null"))
		if !bodyUnrecoverable {
			if _, err := decodeRecipeSteps(value); err != nil {
				return recipe{}, false, fmt.Errorf("invalid body recipe: %w", err)
			}
		}
	}
	return result, bodyUnrecoverable, nil
}

func decodeRecipeSteps(value json.RawMessage) ([]recipeStep, error) {
	var rawSteps []map[string]json.RawMessage
	if err := json.Unmarshal(value, &rawSteps); err != nil {
		return nil, errors.New("recipe steps must be an array")
	}
	steps := make([]recipeStep, 0, len(rawSteps))
	for _, rawStep := range rawSteps {
		if len(rawStep) != 1 {
			return nil, errors.New("recipe step requires exactly one operation")
		}
		var step recipeStep
		for operation, argument := range rawStep {
			switch operation {
			case "c":
				if err := json.Unmarshal(argument, &step.Copy); err != nil {
					return nil, errors.New("invalid recipe copy operation")
				}
			case "d":
				if err := json.Unmarshal(argument, &step.Data); err != nil {
					return nil, errors.New("invalid recipe data operation")
				}
			case "u":
				if err := json.Unmarshal(argument, &step.Binary); err != nil {
					return nil, errors.New("invalid recipe binary operation")
				}
			default:
				return nil, fmt.Errorf("unknown recipe operation %q", operation)
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func reconstructPrevious(headers []field, body []byte, encoded string, limits Limits) ([]field, []byte, error) {
	recipe, bodyUnrecoverable, err := decodeRecipe(encoded, limits)
	if err != nil {
		return nil, nil, err
	}
	if bodyUnrecoverable {
		return nil, nil, errors.New("recipe declares body unrecoverable")
	}
	reconstructedHeaders := append([]field(nil), headers...)
	for name, steps := range recipe.Headers {
		if !validHeaderRecipeName(name) {
			return nil, nil, errors.New("header recipe name must be lowercase")
		}
		var existing []field
		var retained []field
		for _, header := range reconstructedHeaders {
			if strings.EqualFold(header.name, name) {
				existing = append(existing, header)
			} else {
				retained = append(retained, header)
			}
		}
		for left, right := 0, len(existing)-1; left < right; left, right = left+1, right-1 {
			existing[left], existing[right] = existing[right], existing[left]
		}
		values, err := applyHeaderSteps(name, existing, steps)
		if err != nil {
			return nil, nil, err
		}
		reconstructedHeaders = append(retained, values...)
	}
	reconstructedBody := append([]byte(nil), body...)
	if recipe.Body != nil {
		var steps []recipeStep
		if err := json.Unmarshal(recipe.Body, &steps); err != nil {
			return nil, nil, errors.New("invalid body recipe")
		}
		lines := splitBodyLines(body)
		values, err := applyLineSteps(lines, steps)
		if err != nil {
			return nil, nil, err
		}
		reconstructedBody = []byte(strings.Join(values, "\r\n") + "\r\n")
	}
	return reconstructedHeaders, reconstructedBody, nil
}

func validHeaderRecipeName(name string) bool {
	if name == "" || strings.ToLower(name) != name {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func applyHeaderSteps(name string, existing []field, steps []recipeStep) ([]field, error) {
	var output []field
	lastCopy := 0
	for _, step := range steps {
		kind, err := stepKind(step)
		if err != nil {
			return nil, err
		}
		var emitted []field
		switch kind {
		case "c":
			start, end, err := copyRange(step.Copy, len(existing), lastCopy)
			if err != nil {
				return nil, err
			}
			lastCopy = end
			for index := start - 1; index < end; index++ {
				emitted = append(emitted, existing[index])
			}
		case "d", "u":
			values, err := stepValues(kind, step)
			if err != nil {
				return nil, err
			}
			for _, value := range values {
				emitted = append(emitted, field{name: name, value: value})
			}
		}
		output = append(emitted, output...)
	}
	return output, nil
}

func applyLineSteps(existing []string, steps []recipeStep) ([]string, error) {
	var output []string
	lastCopy := 0
	for _, step := range steps {
		kind, err := stepKind(step)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "c":
			start, end, err := copyRange(step.Copy, len(existing), lastCopy)
			if err != nil {
				return nil, err
			}
			lastCopy = end
			output = append(output, existing[start-1:end]...)
		case "d", "u":
			values, err := stepValues(kind, step)
			if err != nil {
				return nil, err
			}
			output = append(output, values...)
		}
	}
	return output, nil
}

func stepKind(step recipeStep) (string, error) {
	count := 0
	kind := ""
	if step.Copy != nil {
		count, kind = count+1, "c"
	}
	if step.Data != nil {
		count, kind = count+1, "d"
	}
	if step.Binary != nil {
		count, kind = count+1, "u"
	}
	if count != 1 {
		return "", errors.New("recipe step requires exactly one operation")
	}
	return kind, nil
}

func copyRange(value []int, available, previous int) (int, int, error) {
	if len(value) != 2 || value[0] < 1 || value[1] < value[0] || value[0] <= previous || value[1] > available {
		return 0, 0, errors.New("invalid recipe copy range")
	}
	return value[0], value[1], nil
}

func stepValues(kind string, step recipeStep) ([]string, error) {
	values := step.Data
	if kind == "u" {
		values = make([]string, 0, len(step.Binary))
		for _, encoded := range step.Binary {
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, errors.New("invalid binary recipe value")
			}
			values = append(values, string(decoded))
		}
	}
	for _, value := range values {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("recipe value contains CR or LF")
		}
	}
	return values, nil
}

func splitBodyLines(body []byte) []string {
	value := strings.TrimSuffix(string(body), "\r\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\r\n")
}
