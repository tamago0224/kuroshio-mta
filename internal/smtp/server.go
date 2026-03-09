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
	"strconv"
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
	bodyMode string
	rcptTo   []string
	data     []byte
	seenHelo bool
	extended bool
	tls      bool
}

type mailFromArgs struct {
	Address string
	Size    int64
	HasSize bool
	Body    string
}

var (
	errMailParamUnsupported = errors.New("unsupported MAIL parameter")
	errSMTPUTF8Param        = errors.New("unsupported SMTPUTF8 parameter")
	errSMTPUTF8Address      = errors.New("smtputf8 address is not supported")
	errDataLineTooLong      = errors.New("data line too long")
	errMessageTooLarge      = errors.New("message too large")
)

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
	ss := &session{remote: conn.RemoteAddr().String(), bodyMode: "7BIT"}
	writeResp(w, 220, s.cfg.Hostname+" ESMTP ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if len(line) > 512 {
			writeResp(w, 500, "command line too long")
			continue
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			writeResp(w, 500, "empty command")
			continue
		}
		verb, arg := splitVerb(line)
		switch verb {
		case "EHLO":
			if arg == "" {
				writeResp(w, 501, "EHLO requires domain")
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
			ss.extended = true
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			writeEHLOResponse(w, s.cfg.Hostname, s.cfg.MaxMessageBytes, s.tlsConfig != nil && !ss.tls)
		case "HELO":
			if arg == "" {
				writeResp(w, 501, "HELO requires domain")
				continue
			}
			ss.helo = arg
			ss.seenHelo = true
			ss.extended = false
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, s.cfg.Hostname)
		case "MAIL":
			if !ss.seenHelo {
				writeResp(w, 503, "send EHLO/HELO first")
				continue
			}
			mailArgs, err := parseMailFrom(arg)
			if err != nil {
				if errors.Is(err, errSMTPUTF8Address) {
					writeResp(w, 553, err.Error())
					continue
				}
				if errors.Is(err, errSMTPUTF8Param) {
					writeResp(w, 555, err.Error())
					continue
				}
				if errors.Is(err, errMailParamUnsupported) {
					writeResp(w, 555, err.Error())
					continue
				}
				writeResp(w, 501, err.Error())
				continue
			}
			if !ss.extended && (mailArgs.HasSize || mailArgs.Body != "") {
				writeResp(w, 555, "MAIL parameters require EHLO")
				continue
			}
			if mailArgs.HasSize && mailArgs.Size > s.cfg.MaxMessageBytes {
				writeResp(w, 552, "message size exceeds fixed maximum message size")
				continue
			}
			if remoteIP != nil && s.flexLimiter != nil && !s.flexLimiter.Allow("mailfrom", remoteIP.String(), ss.helo, mailArgs.Address, time.Now().UTC()) {
				s.metricInc("smtp_reject_rate_limit")
				log.Printf("event=ingress_reject reason=rate_rule_mailfrom remote_ip=%s helo=%s mail_from=%s", remoteIP.String(), ss.helo, mailArgs.Address)
				writeResp(w, 421, "rate limit exceeded, try again later")
				return
			}
			ss.mailFrom = mailArgs.Address
			ss.bodyMode = mailArgs.Body
			if ss.bodyMode == "" {
				ss.bodyMode = "7BIT"
			}
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, "sender ok")
		case "RCPT":
			if ss.mailFrom == "" {
				writeResp(w, 503, "send MAIL FROM first")
				continue
			}
			addr, err := parseRcptTo(arg, s.cfg.Hostname)
			if err != nil {
				if errors.Is(err, errSMTPUTF8Address) {
					writeResp(w, 553, err.Error())
					continue
				}
				if strings.Contains(strings.ToLower(err.Error()), "parameters") {
					writeResp(w, 555, err.Error())
				} else {
					writeResp(w, 501, err.Error())
				}
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
				switch {
				case errors.Is(err, errDataLineTooLong):
					writeResp(w, 500, "line too long")
				case errors.Is(err, errMessageTooLarge):
					writeResp(w, 552, "message size exceeds fixed maximum message size")
				default:
					writeResp(w, 451, "temporary local problem")
				}
				continue
			}
			if !strings.EqualFold(ss.bodyMode, "8BITMIME") && contains8Bit(data) {
				writeResp(w, 554, "8-bit data is not permitted without BODY=8BITMIME")
				ss.mailFrom = ""
				ss.rcptTo = nil
				ss.data = nil
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
			id, err := newID()
			if err != nil {
				writeResp(w, 451, "temporary local problem")
				continue
			}
			received := buildReceivedHeader(s.cfg.Hostname, ss.helo, ss.remote, id, time.Now().UTC(), ss.tls)
			ss.data = mailauth.InjectHeaders(ss.data, []string{received})

			if err := s.enqueue(ss, id); err != nil {
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
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, "reset state")
		case "NOOP":
			writeResp(w, 250, "ok")
		case "HELP":
			writeResp(w, 214, "Supported commands: EHLO HELO MAIL RCPT DATA RSET NOOP QUIT STARTTLS HELP VRFY EXPN")
		case "VRFY":
			writeResp(w, 252, "cannot VRFY user, but will accept message")
		case "EXPN":
			writeResp(w, 502, "EXPN is not supported")
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
			ss.extended = false
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
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

func (s *Server) enqueue(ss *session, id string) error {
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

func parseMailFrom(arg string) (mailFromArgs, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		return mailFromArgs{}, errors.New("MAIL must be MAIL FROM:<addr>")
	}
	path, params, err := splitPathAndParams(strings.TrimSpace(arg[5:]))
	if err != nil {
		return mailFromArgs{}, err
	}
	addr, err := util.NormalizePath(path)
	if err != nil {
		return mailFromArgs{}, err
	}
	if addr != "" && !isASCII(addr) {
		return mailFromArgs{}, errSMTPUTF8Address
	}
	out := mailFromArgs{Address: addr}
	for _, p := range params {
		if strings.EqualFold(strings.TrimSpace(p), "SMTPUTF8") {
			return mailFromArgs{}, fmt.Errorf("%w: SMTPUTF8", errSMTPUTF8Param)
		}
		key, val, ok := splitParamKV(p)
		if !ok {
			return mailFromArgs{}, fmt.Errorf("invalid MAIL parameter: %s", p)
		}
		switch key {
		case "SIZE":
			n, pErr := strconv.ParseInt(val, 10, 64)
			if pErr != nil || n < 0 {
				return mailFromArgs{}, errors.New("invalid SIZE parameter")
			}
			out.HasSize = true
			out.Size = n
		case "BODY":
			v := strings.ToUpper(val)
			if v != "7BIT" && v != "8BITMIME" {
				return mailFromArgs{}, errors.New("invalid BODY parameter")
			}
			out.Body = v
		default:
			return mailFromArgs{}, fmt.Errorf("%w: %s", errMailParamUnsupported, key)
		}
	}
	return out, nil
}

func parseRcptTo(arg string, hostname string) (string, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		return "", errors.New("RCPT must be RCPT TO:<addr>")
	}
	path, params, err := splitPathAndParams(strings.TrimSpace(arg[3:]))
	if err != nil {
		return "", err
	}
	if len(params) > 0 {
		return "", errors.New("RCPT parameters are not supported")
	}
	unwrapped := unwrapPath(path)
	if strings.EqualFold(unwrapped, "postmaster") {
		host := strings.ToLower(strings.TrimSpace(hostname))
		if host == "" {
			host = "localhost"
		}
		return "postmaster@" + host, nil
	}
	addr, err := util.NormalizePath(path)
	if err != nil {
		return "", err
	}
	if addr == "" {
		return "", errors.New("recipient is empty")
	}
	if !isASCII(addr) {
		return "", errSMTPUTF8Address
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
		if len(line) > 1000 {
			return nil, errDataLineTooLong
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
			return nil, errMessageTooLarge
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
		_ = writeLine(w, "250-STARTTLS")
	}
	_ = writeLine(w, "250 HELP")
}

func splitPathAndParams(raw string) (string, []string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, errors.New("missing path")
	}
	if raw[0] == '<' {
		end := strings.IndexByte(raw, '>')
		if end < 0 {
			return "", nil, errors.New("path must be enclosed with <>")
		}
		path := strings.TrimSpace(raw[:end+1])
		rest := strings.TrimSpace(raw[end+1:])
		if rest == "" {
			return path, nil, nil
		}
		return path, strings.Fields(rest), nil
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", nil, errors.New("missing path")
	}
	if len(parts) == 1 {
		return parts[0], nil, nil
	}
	return parts[0], parts[1:], nil
}

func splitParamKV(v string) (key, value string, ok bool) {
	idx := strings.IndexByte(v, '=')
	if idx <= 0 || idx == len(v)-1 {
		return "", "", false
	}
	return strings.ToUpper(strings.TrimSpace(v[:idx])), strings.TrimSpace(v[idx+1:]), true
}

func unwrapPath(path string) string {
	s := strings.TrimSpace(path)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) >= 2 {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
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

func contains8Bit(data []byte) bool {
	for _, b := range data {
		if b >= 0x80 {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return false
		}
	}
	return true
}

func buildReceivedHeader(hostname, helo, remote, id string, now time.Time, tlsOn bool) string {
	by := sanitizeReceivedToken(hostname)
	if by == "" {
		by = "localhost"
	}
	from := sanitizeReceivedToken(helo)
	if from == "" {
		from = "unknown"
	}
	remoteDesc := sanitizeReceivedToken(remote)
	if ip := parseRemoteIP(remote); ip != nil {
		remoteDesc = ip.String()
	}
	proto := "ESMTP"
	if tlsOn {
		proto = "ESMTPS"
	}
	return fmt.Sprintf(
		"Received: from %s (%s) by %s with %s id %s; %s",
		from,
		remoteDesc,
		by,
		proto,
		sanitizeReceivedToken(id),
		now.Format(time.RFC1123Z),
	)
}

func sanitizeReceivedToken(v string) string {
	s := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", ""))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 33 || r > 126:
			b.WriteByte('_')
		case r == '(' || r == ')' || r == ';' || r == '\\' || r == ':':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 255 {
		return out[:255]
	}
	return out
}
