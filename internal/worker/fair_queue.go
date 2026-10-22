package worker

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

type fairQueue struct {
	cfg          config.QueueFairnessConfig
	mu           sync.Mutex
	buffer       map[string]*model.Message
	classCycle   []string
	classCursor  int
	tenantCursor map[string]int
	tenants      map[string]bool
}

func newFairQueue(cfg config.QueueFairnessConfig) *fairQueue {
	if !cfg.Enabled {
		return nil
	}
	cycle := []string{}
	classes := make([]string, 0, len(cfg.ClassWeights))
	for class := range cfg.ClassWeights {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		for range cfg.ClassWeights[class] {
			cycle = append(cycle, class)
		}
	}
	return &fairQueue{cfg: cfg, buffer: map[string]*model.Message{}, classCycle: cycle, tenantCursor: map[string]int{}, tenants: map[string]bool{"default": true, "system": true}}
}

func (f *fairQueue) due(backend queue.Backend, limit int) ([]*model.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fetchLimit := limit * f.cfg.PrefetchFactor
	if remaining := fetchLimit - len(f.buffer); remaining > 0 {
		msgs, err := backend.Due(remaining)
		if err != nil {
			return nil, err
		}
		for _, msg := range msgs {
			if _, exists := f.buffer[msg.ID]; exists {
				continue
			}
			f.normalize(msg)
			f.buffer[msg.ID] = msg
		}
	}
	out := make([]*model.Message, 0, limit)
	classCounts, tenantCounts := map[string]int{}, map[string]int{}
	for len(out) < limit && len(f.buffer) > 0 {
		selected := f.next(classCounts, tenantCounts)
		if selected == nil {
			break
		}
		delete(f.buffer, selected.ID)
		out = append(out, selected)
		classCounts[selected.TrafficClass]++
		tenantCounts[selected.Tenant]++
	}
	return out, nil
}

func (f *fairQueue) normalize(msg *model.Message) {
	tenant := strings.ToLower(strings.TrimSpace(msg.Tenant))
	class := strings.ToLower(strings.TrimSpace(msg.TrafficClass))
	if tenant == "" {
		tenant = "default"
	}
	if !f.tenants[tenant] && len(f.tenants) >= f.cfg.MaxTenants {
		tenant = "default"
	}
	f.tenants[tenant] = true
	if mapped := f.cfg.TenantClasses[tenant]; mapped != "" {
		class = mapped
	}
	if f.cfg.ClassWeights[class] == 0 {
		class = "default"
	}
	if f.cfg.ClassWeights[class] == 0 {
		class = f.classCycle[0]
	}
	msg.Tenant, msg.TrafficClass = tenant, class
}

func (f *fairQueue) next(classCounts, tenantCounts map[string]int) *model.Message {
	for range len(f.classCycle) {
		class := f.classCycle[f.classCursor%len(f.classCycle)]
		f.classCursor++
		if max := f.cfg.ClassMaxPerBatch[class]; max > 0 && classCounts[class] >= max {
			continue
		}
		tenants := map[string][]*model.Message{}
		for _, msg := range f.buffer {
			if msg.TrafficClass == class && (f.cfg.TenantMaxPerBatch == 0 || tenantCounts[msg.Tenant] < f.cfg.TenantMaxPerBatch) {
				tenants[msg.Tenant] = append(tenants[msg.Tenant], msg)
			}
		}
		if len(tenants) == 0 {
			continue
		}
		names := make([]string, 0, len(tenants))
		for tenant := range tenants {
			names = append(names, tenant)
		}
		sort.Strings(names)
		idx := f.tenantCursor[class] % len(names)
		f.tenantCursor[class] = idx + 1
		items := tenants[names[idx]]
		sort.Slice(items, func(i, j int) bool {
			if items[i].NextAttempt.Equal(items[j].NextAttempt) {
				return items[i].ID < items[j].ID
			}
			return items[i].NextAttempt.Before(items[j].NextAttempt)
		})
		return items[0]
	}
	return nil
}

func (f *fairQueue) depth() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.buffer) }

func (f *fairQueue) stats(now time.Time) (map[string]int64, int64, int64, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	classes, tenants := map[string]int64{}, map[string]int64{}
	var oldest int64
	for _, msg := range f.buffer {
		classes[msg.TrafficClass]++
		tenants[msg.Tenant]++
		if !msg.CreatedAt.IsZero() && now.After(msg.CreatedAt) {
			age := int64(now.Sub(msg.CreatedAt).Seconds())
			if age > oldest {
				oldest = age
			}
		}
	}
	var maxTenant int64
	for _, count := range tenants {
		if count > maxTenant {
			maxTenant = count
		}
	}
	return classes, int64(len(tenants)), maxTenant, oldest
}
