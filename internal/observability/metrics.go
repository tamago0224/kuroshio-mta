package observability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type Counter struct {
	name string
	v    atomic.Uint64
}

func (c *Counter) Inc() {
	c.v.Add(1)
}

func (c *Counter) Add(n uint64) {
	c.v.Add(n)
}

func (c *Counter) Load() uint64 {
	return c.v.Load()
}

type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*Counter
}

func NewMetrics() *Metrics {
	return &Metrics{counters: map[string]*Counter{}}
}

func (m *Metrics) Counter(name string) *Counter {
	m.mu.RLock()
	if c, ok := m.counters[name]; ok {
		m.mu.RUnlock()
		return c
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.counters[name]; ok {
		return c
	}
	c := &Counter{name: sanitizeMetricName(name)}
	m.counters[name] = c
	return c
}

func (m *Metrics) Snapshot() map[string]uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]uint64, len(m.counters))
	for k, c := range m.counters {
		out[c.name] = c.Load()
		_ = k
	}
	return out
}

func (m *Metrics) RenderPrometheus() string {
	snap := m.Snapshot()
	names := make([]string, 0, len(snap))
	for n := range snap {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "# TYPE %s counter\n", n)
		fmt.Fprintf(&b, "%s %d\n", n, snap[n])
	}
	return b.String()
}

func sanitizeMetricName(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return "orinoco_unknown_total"
	}
	var b strings.Builder
	for i := 0; i < len(in); i++ {
		ch := in[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if !strings.HasSuffix(name, "_total") {
		name += "_total"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "orinoco_" + name
	}
	return name
}
