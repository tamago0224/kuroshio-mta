package delivery

import (
	"fmt"
	"os"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

type deliveryRoute struct {
	config.DeliveryRoute
	password string
}

type relayAuth struct {
	username string
	password string
}

type relayAuthContextKey struct{}

func loadDeliveryRoutes(routes []config.DeliveryRoute) ([]deliveryRoute, error) {
	out := make([]deliveryRoute, 0, len(routes))
	for _, route := range routes {
		compiled := deliveryRoute{DeliveryRoute: route}
		if route.RelayPasswordFile != "" {
			secret, err := os.ReadFile(route.RelayPasswordFile)
			if err != nil {
				return nil, fmt.Errorf("read relay credential for route %q: %w", route.Name, err)
			}
			compiled.password = strings.TrimRight(string(secret), "\r\n")
			if compiled.password == "" {
				return nil, fmt.Errorf("relay credential for route %q is empty", route.Name)
			}
		}
		out = append(out, compiled)
	}
	return out, nil
}

func (c *Client) routeFor(domain string) (deliveryRoute, bool) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	for _, route := range c.routes {
		for _, configured := range route.Domains {
			configured = strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(configured)), "."), ".")
			if domain == configured || strings.HasSuffix(domain, "."+configured) {
				return route, true
			}
		}
	}
	return deliveryRoute{}, false
}
