package mailauth

import (
	"context"
	"net"

	"github.com/tamago0224/kuroshio-mta/internal/dkim2"
)

// EvaluateDKIM2 runs the experimental verifier for shadow observation. Its
// result is deliberately separate from DKIM1, ARC, DMARC, and policy actions.
func EvaluateDKIM2(ctx context.Context, raw []byte, mailFrom string, recipients []string) DKIM2Result {
	result := dkim2.Verify(ctx, raw, mailFrom, recipients, dkim2.DefaultLimits(), net.DefaultResolver)
	return DKIM2Result{
		Revision:  result.Revision,
		Result:    result.Result,
		Reason:    result.Reason,
		Instances: result.Instances,
		Chain:     result.Chain,
		Queries:   result.Queries,
	}
}
