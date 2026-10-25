package delivery

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

var errSMTPPoolClosed = errors.New("SMTP connection pool is closed")

type smtpPoolKey struct {
	host, sourceIP, ehlo, authIdentity, tlsPolicy string
	port                                          int
}

type smtpSession struct {
	conn       net.Conn
	r          *bufio.Reader
	w          *bufio.Writer
	createdAt  time.Time
	idleSince  time.Time
	deliveries int
	dsn        bool
	key        smtpPoolKey
	keySlots   chan struct{}
	pool       *smtpConnectionPool
}

type smtpConnectionPool struct {
	cfg     config.SMTPConnectionPoolConfig
	metrics *observability.Metrics
	mu      sync.Mutex
	idle    map[smtpPoolKey][]*smtpSession
	keys    map[smtpPoolKey]chan struct{}
	total   chan struct{}
	changed chan struct{}
	closed  bool
}

func newSMTPConnectionPool(cfg config.SMTPConnectionPoolConfig, metrics *observability.Metrics) *smtpConnectionPool {
	if !cfg.Enabled {
		return nil
	}
	return &smtpConnectionPool{cfg: cfg, metrics: metrics, idle: make(map[smtpPoolKey][]*smtpSession), keys: make(map[smtpPoolKey]chan struct{}), total: make(chan struct{}, cfg.MaxConnections), changed: make(chan struct{}, 1)}
}

func (p *smtpConnectionPool) get(ctx context.Context, key smtpPoolKey, dial func() (*smtpSession, error)) (*smtpSession, error) {
	for {
		now := time.Now()
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, errSMTPPoolClosed
		}
		list := p.idle[key]
		for len(list) > 0 {
			s := list[len(list)-1]
			list = list[:len(list)-1]
			p.idle[key] = list
			if now.Sub(s.idleSince) <= p.cfg.IdleTimeout && now.Sub(s.createdAt) <= p.cfg.MaxLifetime && s.deliveries < p.cfg.MaxDeliveries {
				p.mu.Unlock()
				p.metric("smtp_pool_reused")
				return s, nil
			}
			p.closeSessionLocked(s)
		}
		keySlots := p.keys[key]
		if keySlots == nil {
			keySlots = make(chan struct{}, p.cfg.MaxConnectionsKey)
			p.keys[key] = keySlots
		}
		p.mu.Unlock()

		select {
		case keySlots <- struct{}{}:
			select {
			case p.total <- struct{}{}:
				s, err := dial()
				if err != nil {
					<-p.total
					<-keySlots
					p.signalChanged()
					return nil, err
				}
				s.key, s.keySlots, s.pool = key, keySlots, p
				p.metric("smtp_pool_dialed")
				return s, nil
			default:
				<-keySlots
			}
		default:
		}
		select {
		case <-p.changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (p *smtpConnectionPool) put(s *smtpSession, reusable bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s.deliveries++
	now := time.Now()
	list := p.idle[s.key]
	if !reusable || p.closed || now.Sub(s.createdAt) >= p.cfg.MaxLifetime || s.deliveries >= p.cfg.MaxDeliveries || len(list) >= p.cfg.MaxIdleKey || p.idleCountLocked() >= p.cfg.MaxIdle {
		p.closeSessionLocked(s)
		return
	}
	s.idleSince = now
	p.idle[s.key] = append(list, s)
	if p.metrics != nil {
		p.metrics.SetGauge("smtp_pool_idle_connections", int64(p.idleCountLocked()))
	}
	p.signalChanged()
}

func (p *smtpConnectionPool) closeSessionLocked(s *smtpSession) {
	_ = s.conn.Close()
	if s.keySlots != nil {
		<-s.keySlots
		<-p.total
		s.keySlots = nil
	}
	p.metric("smtp_pool_closed")
	if p.metrics != nil {
		p.metrics.SetGauge("smtp_pool_open_connections", int64(len(p.total)))
	}
	p.signalChanged()
}

func (p *smtpConnectionPool) signalChanged() {
	select {
	case p.changed <- struct{}{}:
	default:
	}
}

func (p *smtpConnectionPool) idleCountLocked() int {
	n := 0
	for _, sessions := range p.idle {
		n += len(sessions)
	}
	return n
}

func (p *smtpConnectionPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for key, sessions := range p.idle {
		for _, s := range sessions {
			p.closeSessionLocked(s)
		}
		delete(p.idle, key)
	}
	p.signalChanged()
	return nil
}

func (p *smtpConnectionPool) metric(name string) {
	if p.metrics != nil {
		p.metrics.Counter(name).Inc()
		p.metrics.SetGauge("smtp_pool_open_connections", int64(len(p.total)))
	}
}
