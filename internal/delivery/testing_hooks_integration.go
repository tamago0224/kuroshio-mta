//go:build integration

package delivery

import (
	"context"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/router"
)

func (c *Client) ResolveForTest(
	resolveMX func(string, time.Duration) ([]router.MXHost, error),
	lookupMTASTS func(string) (MTASTSPolicy, error),
) {
	if resolveMX != nil {
		c.resolveMXFn = resolveMX
	}
	if lookupMTASTS != nil {
		c.mtaSTS = NewMTASTSResolver(time.Minute, time.Second, func(_ context.Context, domain string) (string, error) {
			p, err := lookupMTASTS(domain)
			if err != nil {
				return "", err
			}
			raw := "version: " + p.Version + "\nmode: " + p.Mode + "\n"
			for _, mx := range p.MX {
				raw += "mx: " + mx + "\n"
			}
			raw += "max_age: 3600\n"
			return raw, nil
		})
	}
}

func (c *Client) DeliverHostForTest(
	fn func(host string, port int, msg *model.Message, rcpt string, requireTLS bool, _ *DANEResult) error,
) {
	if fn == nil {
		return
	}
	c.deliverHostFn = func(_ context.Context, host string, port int, msg *model.Message, rcpt string, requireTLS bool, _ *DANEResult) error {
		return fn(host, port, msg, rcpt, requireTLS, nil)
	}
}
