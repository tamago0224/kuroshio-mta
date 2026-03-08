package auth

import (
	"fmt"
	"strconv"
	"strings"
)

func EvalARC(headers []Header) ARCResult {
	seals := HeaderValues(headers, "ARC-Seal")
	msgs := HeaderValues(headers, "ARC-Message-Signature")
	aars := HeaderValues(headers, "ARC-Authentication-Results")
	if len(seals) == 0 && len(msgs) == 0 && len(aars) == 0 {
		return ARCResult{Result: "none"}
	}
	if len(seals) != len(msgs) || len(seals) != len(aars) {
		return ARCResult{Result: "fail", Reason: "arc set counts mismatch"}
	}
	n := len(seals)
	for i := 1; i <= n; i++ {
		if !hasInstance(seals, i) || !hasInstance(msgs, i) || !hasInstance(aars, i) {
			return ARCResult{Result: "fail", Reason: fmt.Sprintf("missing instance i=%d", i)}
		}
	}
	if !strings.Contains(strings.ToLower(seals[n-1]), "cv=") {
		return ARCResult{Result: "fail", Reason: "missing cv in latest seal"}
	}
	return ARCResult{Result: "pass", Reason: "arc chain structure valid"}
}

func hasInstance(values []string, want int) bool {
	for _, v := range values {
		tags := parseTagList(v)
		i, _ := strconv.Atoi(tags["i"])
		if i == want {
			return true
		}
	}
	return false
}
