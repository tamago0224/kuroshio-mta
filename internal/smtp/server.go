package smtp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/ingress"
	"github.com/tamago0224/orinoco-mta/internal/mailauth"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/observability"
	"github.com/tamago0224/orinoco-mta/internal/queue"
	"github.com/tamago0224/orinoco-mta/internal/util"
)

type Server struct {
	cfg         config.Config
	queue       queue.Backend
	tlsConfig   *tls.Config
	tlsLoadErr  error
	limiter     *ingress.RateLimiter
	flexLimiter *ingress.FlexibleLimiter
	rateRuleErr error
	dnsbl       *ingress.DNSBLChecker
	metrics     *observability.Metrics
	ln          net.Listener
	wg          sync.WaitGroup
}

func NewServer(cfg config.Config, q queue.Backend, metrics *observability.Metrics) *Server {
	tlsConfig, err := loadTLSConfig(cfg)
	rules, rErr := ingress.ParseRateRules(cfg.RateLimitRules)
	return &Server{
		cfg:         cfg,
		queue:       q,
		tlsConfig:   tlsConfig,
		tlsLoadErr:  err,
		limiter:     ingress.NewRateLimiter(cfg.IngressRateLimit, time.Minute),
		flexLimiter: ingress.NewFlexibleLimiter(rules),
		rateRuleErr: rErr,
		dnsbl:       ingress.NewDNSBLChecker(cfg.DNSBLZones, cfg.DNSBLCacheTTL, nil),
		metrics:     metrics,
	}
}

func (s *Server) Run(ctx context.Context) error {
	if s.tlsLoadErr != nil {
		return s.tlsLoadErr
	}
	if s.rateRuleErr != nil {
		return s.rateRuleErr
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.ln = ln
	log.Printf("smtp listening on %s", s.cfg.ListenAddr)

	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				break
			}
			log.Printf("accept error: %v", err)
			continue
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConn(c)
		}(conn)
	}
	s.wg.Wait()
	return nil
}

type session struct {
	remote   string
	helo     string
	mailFrom string
	rcptTo   []string
	data     []byte
	seenHelo bool
	tls      bool
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
	s.metricInc("smtp_connections")
	remoteIP := parseRemoteIP(conn.RemoteAddr().String())
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if remoteIP != nil {
		remoteStr := remoteIP.String()
		now := time.Now().UTC()
		if s.limiter != nil && !s.limiter.Allow(remoteStr, now) {
			s.metricInc("smtp_reject_rate_limit")
			log.Printf("event=ingress_reject reason=rate_limit remote_ip=%s", remoteStr)
			writeResp(w, 421, "rate limit exceeded, try again later")
			return
		}
		if s.flexLimiter != nil && !s.flexLimiter.Allow("connect", remoteStr, "", "", now) {
			s.metricInc("smtp_reject_rate_limit")
			log.Printf("event=ingress_reject reason=rate_rule_connect remote_ip=%s", remoteStr)
			writeResp(w, 421, "rate limit exceeded, try again later")
			return
		}
		if s.dnsbl != nil {
			if listed, zone := s.dnsbl.IsListed(remoteStr); listed {
				s.metricInc("smtp_reject_dnsbl")
				log.Printf("event=ingress_reject reason=dnsbl zone=%s remote_ip=%s", zone, remoteStr)
				writeResp(w, 554, "connection rejected (dnsbl: "+zone+")")
				return
			}
		}
	}
	ss := &session{remote: conn.RemoteAddr().String()}
	writeResp(w, 220, s.cfg.Hostname+" ESMTP ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			writeResp(w, 500, "empty command")
			continue
		}
		verb, arg := splitVerb(line)
		switch verb {
		case "EHLO", "HELO":
			if arg == "" {
				writeResp(w, 501, verb+" requires domain")
				continue
			}
			ss.helo = arg
			if remoteIP != nil && s.flexLimiter != nil && !s.flexLimiter.Allow("helo", remoteIP.String(), ss.helo, "", time.Now().UTC()) {
				s.metricInc("smtp_reject_rate_limit")
				log.Printf("event=ingress_reject reason=rate_rule_helo remote_ip=%s helo=%s", remoteIP.String(), ss.helo)
				writeResp(w, 421, "rate limit exceeded, try again later")
				return
			}
			ss.seenHelo = true
			ss.mailFrom = ""
			ss.rcptTo = nil
			ss.data = nil
			writeEHLOResponse(w, s.cfg.Hostname, s.cfg.MaxMessageBytes, s.tlsConfig != nil && !ss.tls)
		case "MAIL":
			if !ss.seenHelo {
				writeResp(w, 503, "send EHLO/HELO first")
				continue
			}
			addr, err := parseMailFrom(arg)
			if err != nil {
				writeResp(w, 501, err.Error())
				continue
			}
			if remoteIP != nil && s.flexLimiter != nil && !s.flexLimiter.Allow("mailfrom", remoteIP.String(), ss.helo, addr, time.Now().UTC()) {
				s.metricInc("smtp_reject_rate_limit")
				log.Printf("event=ingress_reject reason=rate_rule_mailfrom remote_ip=%s helo=%s mail_from=%s", remoteIP.String(), ss.helo, addr)
				writeResp(w, 421, "rate limit exceeded, try again later")
				return
			}
			ss.mailFrom = addr
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, "sender ok")
		case "RCPT":
			if ss.mailFrom == "" {
				writeResp(w, 503, "send MAIL FROM first")
				continue
			}
			addr, err := parseRcptTo(arg)
			if err != nil {
				writeResp(w, 501, err.Error())
				continue
			}
			ss.rcptTo = append(ss.rcptTo, addr)
			writeResp(w, 250, "recipient ok")
		case "DATA":
			if len(ss.rcptTo) == 0 {
				writeResp(w, 503, "need RCPT TO before DATA")
				continue
			}
			writeResp(w, 354, "end with <CRLF>.<CRLF>")
			data, err := readData(r, s.cfg.MaxMessageBytes)
			if err != nil {
				writeResp(w, 552, "message exceeds limit or read error")
				continue
			}
			ss.data = data
			msgRemoteIP := remoteIP
			if msgRemoteIP == nil {
				msgRemoteIP = parseRemoteIP(ss.remote)
			}
			authRes := mailauth.Evaluate(msgRemoteIP, ss.helo, ss.mailFrom, ss.data)
			switch authRes.Action {
			case mailauth.ActionReject:
				writeResp(w, 550, "message rejected by auth policy")
				ss.mailFrom = ""
				ss.rcptTo = nil
				ss.data = nil
				continue
			case mailauth.ActionQuarantine:
				ar := mailauth.BuildAuthResultsHeader(s.cfg.Hostname, authRes, ss.mailFrom)
				ss.data = mailauth.InjectHeaders(ss.data, []string{ar, "X-Orinoco-Quarantine: true"})
			default:
				ar := mailauth.BuildAuthResultsHeader(s.cfg.Hostname, authRes, ss.mailFrom)
				ss.data = mailauth.InjectHeaders(ss.data, []string{ar})
			}
			if err := s.enqueue(ss); err != nil {
				log.Printf("enqueue error: %v", err)
				s.metricInc("smtp_enqueue_fail")
				writeResp(w, 451, "temporary local problem")
				continue
			}
			s.metricInc("smtp_queued_messages")
			writeResp(w, 250, "queued")
			ss.mailFrom = ""
			ss.rcptTo = nil
			ss.data = nil
		case "RSET":
			ss.mailFrom = ""
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, "reset state")
		case "NOOP":
			writeResp(w, 250, "ok")
		case "QUIT":
			writeResp(w, 221, "bye")
			return
		case "STARTTLS":
			if ss.tls {
				writeResp(w, 503, "already using TLS")
				continue
			}
			if s.tlsConfig == nil {
				writeResp(w, 454, "TLS not available due to temporary reason")
				continue
			}
			writeResp(w, 220, "Ready to start TLS")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			ss.tls = true
			ss.seenHelo = false
			ss.mailFrom = ""
			ss.rcptTo = nil
			ss.data = nil
		default:
			writeResp(w, 500, "unsupported command")
		}
	}
}

func (s *Server) metricInc(name string) {
	if s.metrics != nil {
		s.metrics.Counter(name).Inc()
	}
}

func (s *Server) enqueue(ss *session) error {
	id, err := newID()
	if err != nil {
		return err
	}
	msg := &model.Message{
		ID:         id,
		RemoteAddr: ss.remote,
		Helo:       ss.helo,
		MailFrom:   ss.mailFrom,
		RcptTo:     append([]string(nil), ss.rcptTo...),
		Data:       append([]byte(nil), ss.data...),
	}
	return s.queue.Enqueue(msg)
}

func splitVerb(line string) (string, string) {
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 2)
	verb := strings.ToUpper(parts[0])
	if len(parts) == 1 {
		return verb, ""
	}
	return verb, strings.TrimSpace(parts[1])
}

func parseMailFrom(arg string) (string, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		return "", errors.New("MAIL must be MAIL FROM:<addr>")
	}
	path := strings.TrimSpace(arg[5:])
	addr, err := util.NormalizePath(path)
	if err != nil {
		return "", err
	}
	return addr, nil
}

func parseRcptTo(arg string) (string, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		return "", errors.New("RCPT must be RCPT TO:<addr>")
	}
	path := strings.TrimSpace(arg[3:])
	addr, err := util.NormalizePath(path)
	if err != nil {
		return "", err
	}
	if addr == "" {
		return "", errors.New("recipient is empty")
	}
	return addr, nil
}

func readData(r *bufio.Reader, maxBytes int64) ([]byte, error) {
	var out []byte
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		out = append(out, []byte(line)...)
		out = append(out, '\r', '\n')
		if int64(len(out)) > maxBytes {
			return nil, errors.New("message too large")
		}
	}
	return out, nil
}

func writeResp(w *bufio.Writer, code int, msg string) {
	_ = writeLine(w, fmt.Sprintf("%d %s", code, msg))
}

func writeLine(w *bufio.Writer, line string) error {
	if _, err := w.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return w.Flush()
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func parseRemoteIP(remote string) net.IP {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return net.ParseIP(remote)
	}
	return net.ParseIP(host)
}

func writeEHLOResponse(w *bufio.Writer, hostname string, maxMessageBytes int64, advertiseStartTLS bool) {
	_ = writeLine(w, "250-"+hostname)
	_ = writeLine(w, "250-PIPELINING")
	_ = writeLine(w, fmt.Sprintf("250-SIZE %d", maxMessageBytes))
	_ = writeLine(w, "250-8BITMIME")
	if advertiseStartTLS {
		_ = writeLine(w, "250 STARTTLS")
		return
	}
	_ = writeLine(w, "250 HELP")
}

func loadTLSConfig(cfg config.Config) (*tls.Config, error) {
	if cfg.TLSCertFile == "" && cfg.TLSKeyFile == "" {
		return nil, nil
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, errors.New("both MTA_TLS_CERT_FILE and MTA_TLS_KEY_FILE must be set")
	}
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS cert/key: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
