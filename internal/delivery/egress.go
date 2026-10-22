package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/shaping"
)

type egressContextKey struct{}
type egressSelection struct{ name, ip, ehlo string }
type egressUsage struct {
	Date   string         `json:"date"`
	Counts map[string]int `json:"counts"`
}

type egressManager struct {
	mu          sync.Mutex
	sources     map[string]config.EgressSource
	cycles      map[string][]string
	cursors     map[string]int
	tenantPools map[string]string
	path        string
	usage       egressUsage
	now         func() time.Time
}

var interfaceAddrs = net.InterfaceAddrs

func newEgressManager(cfg config.Config) (*egressManager, error) {
	if len(cfg.EgressSources) == 0 {
		return nil, nil
	}
	assigned, err := interfaceAddrs()
	if err != nil {
		return nil, errors.New("list local addresses for egress sources")
	}
	local := map[string]bool{}
	for _, addr := range assigned {
		if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
			local[ip.String()] = true
		}
	}
	m := &egressManager{sources: map[string]config.EgressSource{}, cycles: map[string][]string{}, cursors: map[string]int{}, tenantPools: map[string]string{}, path: filepath.Join(cfg.QueueDir, "egress-source-usage.json"), now: time.Now}
	for tenant, pool := range cfg.EgressTenantPools {
		m.tenantPools[strings.ToLower(strings.TrimSpace(tenant))] = pool
	}
	for _, source := range cfg.EgressSources {
		if !local[net.ParseIP(source.IP).String()] {
			return nil, fmt.Errorf("egress source %q IP is not assigned", source.Name)
		}
		m.sources[source.Name] = source
	}
	for _, pool := range cfg.EgressPools {
		for _, name := range pool.Sources {
			for range m.sources[name].Weight {
				m.cycles[pool.Name] = append(m.cycles[pool.Name], name)
			}
		}
	}
	if data, err := os.ReadFile(m.path); err == nil {
		if json.Unmarshal(data, &m.usage) != nil || m.usage.Counts == nil {
			return nil, errors.New("invalid egress usage state")
		}
		for name, count := range m.usage.Counts {
			if _, ok := m.sources[name]; !ok || count < 0 {
				return nil, errors.New("invalid egress usage state")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("read egress usage state")
	}
	if m.usage.Counts == nil {
		m.usage.Counts = map[string]int{}
	}
	return m, nil
}

func (m *egressManager) poolFor(routePool, tenant string) string {
	if pool := m.tenantPools[strings.ToLower(strings.TrimSpace(tenant))]; pool != "" {
		return pool
	}
	return routePool
}

func (m *egressManager) selectSource(pool string) (egressSelection, error) {
	if m == nil || pool == "" {
		return egressSelection{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cycle := m.cycles[pool]
	if len(cycle) == 0 {
		return egressSelection{}, errors.New("egress pool is empty")
	}
	now := m.now().UTC()
	date := now.Format("2006-01-02")
	if m.usage.Date != date {
		m.usage = egressUsage{Date: date, Counts: map[string]int{}}
	}
	name := cycle[m.cursors[pool]%len(cycle)]
	m.cursors[pool]++
	source := m.sources[name]
	if source.Stopped || source.DailyLimit > 0 && m.usage.Counts[name] >= source.DailyLimit {
		delay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC).Sub(now)
		return egressSelection{}, &shaping.DeferredError{Delay: delay, Reason: "egress source unavailable: " + name}
	}
	m.usage.Counts[name]++
	if err := m.saveLocked(); err != nil {
		m.usage.Counts[name]--
		return egressSelection{}, err
	}
	return egressSelection{name: name, ip: source.IP, ehlo: strings.TrimSuffix(source.EHLO, ".")}, nil
}

func (m *egressManager) saveLocked() error {
	data, err := json.Marshal(m.usage)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".egress-usage-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.path)
}
