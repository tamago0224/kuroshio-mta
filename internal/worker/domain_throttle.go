package worker

import (
	"strings"
	"sync"
	"time"
)

type domainThrottle struct {
	defLimit   int
	rules      map[string]int
	adaptive   bool
	threshold  float64
	maxPenalty time.Duration
	mu         sync.Mutex
	perDomain  map[string]*domainState
}

type domainState struct {
	sem      chan struct{}
	penalty  time.Duration
	samples  int
	tempFail int
}

func newDomainThrottle(defLimit int, rules map[string]int, adaptive bool, threshold float64, maxPenalty time.Duration) *domainThrottle {
	if defLimit <= 0 {
		defLimit = 1
	}
	if threshold <= 0 || threshold >= 1 {
		threshold = 0.3
	}
	if maxPenalty <= 0 {
		maxPenalty = 5 * time.Second
	}
	return &domainThrottle{
		defLimit:   defLimit,
		rules:      rules,
		adaptive:   adaptive,
		threshold:  threshold,
		maxPenalty: maxPenalty,
		perDomain:  map[string]*domainState{},
	}
}

func (d *domainThrottle) acquire(domain string) func() {
	domain = strings.ToLower(strings.TrimSpace(domain))
	st := d.get(domain)
	if p := d.currentPenalty(st); p > 0 {
		time.Sleep(p)
	}
	st.sem <- struct{}{}
	return func() { <-st.sem }
}

func (d *domainThrottle) observe(domain string, temporaryFailure bool) {
	if !d.adaptive {
		return
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	st := d.get(domain)
	d.mu.Lock()
	defer d.mu.Unlock()
	st.samples++
	if temporaryFailure {
		st.tempFail++
	}
	if st.samples < 20 {
		return
	}
	ratio := float64(st.tempFail) / float64(st.samples)
	if ratio >= d.threshold {
		if st.penalty == 0 {
			st.penalty = 200 * time.Millisecond
		} else {
			st.penalty *= 2
		}
		if st.penalty > d.maxPenalty {
			st.penalty = d.maxPenalty
		}
	} else {
		st.penalty /= 2
		if st.penalty < 50*time.Millisecond {
			st.penalty = 0
		}
	}
	st.samples = 0
	st.tempFail = 0
}

func (d *domainThrottle) get(domain string) *domainState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st, ok := d.perDomain[domain]; ok {
		return st
	}
	limit := d.defLimit
	if v, ok := d.rules[domain]; ok && v > 0 {
		limit = v
	}
	st := &domainState{sem: make(chan struct{}, limit)}
	d.perDomain[domain] = st
	return st
}

func (d *domainThrottle) currentPenalty(st *domainState) time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return st.penalty
}

func parseDomainRules(raw string) map[string]int {
	out := map[string]int{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		i := strings.IndexByte(p, ':')
		if i <= 0 || i == len(p)-1 {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(p[:i]))
		limit := strings.TrimSpace(p[i+1:])
		n := 0
		for _, ch := range limit {
			if ch < '0' || ch > '9' {
				n = 0
				break
			}
			n = n*10 + int(ch-'0')
		}
		if domain != "" && n > 0 {
			out[domain] = n
		}
	}
	return out
}
