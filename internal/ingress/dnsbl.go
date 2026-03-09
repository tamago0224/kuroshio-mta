package ingress

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type lookupHostFunc func(string) ([]string, error)

type dnsblCacheEntry struct {
	listed    bool
	zone      string
	expiresAt time.Time
}

type DNSBLChecker struct {
	zones  []string
	ttl    time.Duration
	lookup lookupHostFunc
	mu     sync.Mutex
	cache  map[string]dnsblCacheEntry
}

func NewDNSBLChecker(zones []string, ttl time.Duration, lookup lookupHostFunc) *DNSBLChecker {
	if lookup == nil {
		lookup = net.LookupHost
	}
	return &DNSBLChecker{
		zones:  append([]string(nil), zones...),
		ttl:    ttl,
		lookup: lookup,
		cache:  map[string]dnsblCacheEntry{},
	}
}

func (d *DNSBLChecker) IsListed(ip string) (bool, string) {
	if d == nil || len(d.zones) == 0 {
		return false, ""
	}
	now := time.Now().UTC()
	d.mu.Lock()
	if c, ok := d.cache[ip]; ok && now.Before(c.expiresAt) {
		d.mu.Unlock()
		return c.listed, c.zone
	}
	d.mu.Unlock()

	rev, ok := reverseIPv4(ip)
	if !ok {
		d.setCache(ip, dnsblCacheEntry{listed: false, expiresAt: now.Add(d.ttl)})
		return false, ""
	}
	for _, zone := range d.zones {
		q := fmt.Sprintf("%s.%s", rev, zone)
		addrs, err := d.lookup(q)
		if err == nil && len(addrs) > 0 {
			entry := dnsblCacheEntry{listed: true, zone: zone, expiresAt: now.Add(d.ttl)}
			d.setCache(ip, entry)
			return true, zone
		}
	}
	d.setCache(ip, dnsblCacheEntry{listed: false, expiresAt: now.Add(d.ttl)})
	return false, ""
}

func (d *DNSBLChecker) setCache(ip string, c dnsblCacheEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache[ip] = c
}

func reverseIPv4(ip string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(ip), ".")
	if len(parts) != 4 {
		return "", false
	}
	return parts[3] + "." + parts[2] + "." + parts[1] + "." + parts[0], true
}
