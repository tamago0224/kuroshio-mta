package router

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
)

type MXHost struct {
	Host string
	Pref uint16
}

func ResolveMX(ctx context.Context, domain string) ([]MXHost, error) {
	resolver := net.Resolver{}
	mx, err := resolver.LookupMX(ctx, domain)
	if err != nil || len(mx) == 0 {
		addrs, aErr := resolver.LookupHost(ctx, domain)
		if aErr != nil || len(addrs) == 0 {
			return nil, err
		}
		return []MXHost{{Host: strings.TrimSuffix(domain, "."), Pref: 0}}, nil
	}
	hosts := make([]MXHost, 0, len(mx))
	for _, m := range mx {
		hosts = append(hosts, MXHost{Host: strings.TrimSuffix(m.Host, "."), Pref: m.Pref})
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Pref == hosts[j].Pref {
			return hosts[i].Host < hosts[j].Host
		}
		return hosts[i].Pref < hosts[j].Pref
	})
	return hosts, nil
}

func LookupWithTimeout(domain string, timeout time.Duration) ([]MXHost, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return ResolveMX(ctx, domain)
}
