package delivery

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const (
	dnsTypeTLSA = 52
	dnsClassIN  = 1
)

type TLSARecord struct {
	Usage                  uint8
	Selector               uint8
	MatchingType           uint8
	CertificateAssociation []byte
}

type DANEResult struct {
	AuthenticatedData bool
	Records           []TLSARecord
}

func (r DANEResult) HasUsableTLSA() bool {
	return r.HasUsableTLSAWithTrustModel("ad_required")
}

func (r DANEResult) HasUsableTLSAWithTrustModel(trustModel string) bool {
	trustModel = strings.ToLower(strings.TrimSpace(trustModel))
	switch trustModel {
	case "", "ad_required":
		if !r.AuthenticatedData {
			return false
		}
	case "insecure_allow_unsigned":
		// allow AD=false and evaluate only TLSA profile validity.
	default:
		if !r.AuthenticatedData {
			return false
		}
	}
	for _, rec := range r.Records {
		if isSupportedTLSAProfile(rec) {
			return true
		}
	}
	return false
}

func isSupportedTLSAProfile(rec TLSARecord) bool {
	if len(rec.CertificateAssociation) == 0 {
		return false
	}
	switch rec.Usage {
	case 2, 3: // DANE-TA, DANE-EE
	default:
		return false
	}
	switch rec.Selector {
	case 0, 1: // Full cert, SPKI
	default:
		return false
	}
	switch rec.MatchingType {
	case 1, 2: // SHA-256, SHA-512
		return true
	default:
		return false
	}
}

func verifyPeerCertificatesWithTLSA(peerCerts []*x509.Certificate, records []TLSARecord) error {
	if len(peerCerts) == 0 {
		return errors.New("no peer certificates")
	}
	if len(records) == 0 {
		return errors.New("no tlsa records")
	}
	for _, rec := range records {
		if !isSupportedTLSAProfile(rec) {
			continue
		}
		candidates := peerCertCandidatesForUsage(peerCerts, rec.Usage)
		for _, cert := range candidates {
			if matchTLSARecord(cert, rec) {
				return nil
			}
		}
	}
	return errors.New("no matching tlsa record for peer certificates")
}

func peerCertCandidatesForUsage(peerCerts []*x509.Certificate, usage uint8) []*x509.Certificate {
	switch usage {
	case 3: // DANE-EE: leaf certificate.
		return peerCerts[:1]
	case 2: // DANE-TA: trust anchor candidates from chain.
		return peerCerts
	default:
		return nil
	}
}

func matchTLSARecord(cert *x509.Certificate, rec TLSARecord) bool {
	var selected []byte
	switch rec.Selector {
	case 0:
		selected = cert.Raw
	case 1:
		selected = cert.RawSubjectPublicKeyInfo
	default:
		return false
	}

	var digested []byte
	switch rec.MatchingType {
	case 1:
		sum := sha256.Sum256(selected)
		digested = sum[:]
	case 2:
		sum := sha512.Sum512(selected)
		digested = sum[:]
	default:
		return false
	}
	return subtle.ConstantTimeCompare(digested, rec.CertificateAssociation) == 1
}

type DANEResolver struct {
	timeout       time.Duration
	lookupFn      func(context.Context, string, int) (DANEResult, error)
	lookupCNAMEFn func(context.Context, string) (string, error)
}

func NewDANEResolver(timeout time.Duration, lookupFn func(context.Context, string, int) (DANEResult, error)) *DANEResolver {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if lookupFn == nil {
		lookupFn = lookupTLSAUDP(timeout)
	}
	return &DANEResolver{
		timeout:       timeout,
		lookupFn:      lookupFn,
		lookupCNAMEFn: lookupCNAME,
	}
}

func (r *DANEResolver) LookupHost(ctx context.Context, host string, port int) (DANEResult, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return DANEResult{}, errors.New("empty host")
	}
	if port <= 0 {
		port = 25
	}
	candidates := r.expandDANECandidates(ctx, host)
	merged := DANEResult{}
	var lastErr error
	for _, cand := range candidates {
		res, err := r.lookupFn(ctx, cand, port)
		if err != nil {
			lastErr = err
			continue
		}
		if res.AuthenticatedData {
			merged.AuthenticatedData = true
		}
		if len(res.Records) > 0 {
			merged.Records = append(merged.Records, res.Records...)
		}
	}
	if len(merged.Records) > 0 {
		return merged, nil
	}
	if lastErr != nil {
		return DANEResult{}, lastErr
	}
	return DANEResult{}, nil
}

func (r *DANEResolver) expandDANECandidates(ctx context.Context, host string) []string {
	out := []string{host}
	if r.lookupCNAMEFn == nil {
		return out
	}
	seen := map[string]struct{}{host: {}}
	current := host
	const maxDepth = 5
	for i := 0; i < maxDepth; i++ {
		cname, err := r.lookupCNAMEFn(ctx, current)
		if err != nil {
			break
		}
		cname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(cname)), ".")
		if cname == "" || cname == current {
			break
		}
		if _, ok := seen[cname]; ok {
			break
		}
		seen[cname] = struct{}{}
		out = append(out, cname)
		current = cname
	}
	return out
}

func lookupCNAME(ctx context.Context, host string) (string, error) {
	return net.DefaultResolver.LookupCNAME(ctx, host)
}

func lookupTLSAUDP(timeout time.Duration) func(context.Context, string, int) (DANEResult, error) {
	return func(ctx context.Context, host string, port int) (DANEResult, error) {
		qname := fmt.Sprintf("_%d._tcp.%s", port, host)
		packet, queryID, err := buildDNSQuery(qname, dnsTypeTLSA)
		if err != nil {
			return DANEResult{}, err
		}

		servers := systemDNSServers()
		var lastErr error
		for _, server := range servers {
			dialer := &net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
			if err != nil {
				lastErr = err
				continue
			}
			_ = conn.SetDeadline(time.Now().Add(timeout))
			_, err = conn.Write(packet)
			if err != nil {
				lastErr = err
				_ = conn.Close()
				continue
			}

			buf := make([]byte, 4096)
			n, err := conn.Read(buf)
			_ = conn.Close()
			if err != nil {
				lastErr = err
				continue
			}
			return parseTLSAResponse(buf[:n], queryID)
		}
		if lastErr == nil {
			lastErr = errors.New("no dns servers available")
		}
		return DANEResult{}, lastErr
	}
}

func systemDNSServers() []string {
	content, err := os.ReadFile("/etc/resolv.conf")
	if err == nil {
		lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
		out := make([]string, 0, 2)
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "nameserver ") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] != "" {
				out = append(out, parts[1])
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{"127.0.0.53", "8.8.8.8"}
}

func buildDNSQuery(qname string, qtype uint16) ([]byte, uint16, error) {
	labels := strings.Split(strings.Trim(strings.TrimSpace(qname), "."), ".")
	if len(labels) == 0 {
		return nil, 0, errors.New("empty qname")
	}
	id := uint16(time.Now().UTC().UnixNano())
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], id)
	binary.BigEndian.PutUint16(header[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(header[4:6], 1)      // QDCOUNT

	q := make([]byte, 0, 256)
	q = append(q, header...)
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return nil, 0, errors.New("invalid dns label")
		}
		q = append(q, byte(len(label)))
		q = append(q, []byte(label)...)
	}
	q = append(q, 0x00)
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint16(tmp[0:2], qtype)
	binary.BigEndian.PutUint16(tmp[2:4], dnsClassIN)
	q = append(q, tmp...)
	return q, id, nil
}

func parseTLSAResponse(packet []byte, queryID uint16) (DANEResult, error) {
	if len(packet) < 12 {
		return DANEResult{}, errors.New("short dns packet")
	}
	if binary.BigEndian.Uint16(packet[0:2]) != queryID {
		return DANEResult{}, errors.New("dns id mismatch")
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	rcode := flags & 0x000f
	if rcode != 0 {
		return DANEResult{}, fmt.Errorf("dns rcode=%d", rcode)
	}
	ad := (flags & 0x0020) != 0

	qd := int(binary.BigEndian.Uint16(packet[4:6]))
	an := int(binary.BigEndian.Uint16(packet[6:8]))
	offset := 12

	for i := 0; i < qd; i++ {
		var err error
		offset, err = skipDNSName(packet, offset)
		if err != nil {
			return DANEResult{}, err
		}
		if len(packet) < offset+4 {
			return DANEResult{}, errors.New("short dns question")
		}
		offset += 4
	}

	records := make([]TLSARecord, 0, an)
	for i := 0; i < an; i++ {
		var err error
		offset, err = skipDNSName(packet, offset)
		if err != nil {
			return DANEResult{}, err
		}
		if len(packet) < offset+10 {
			return DANEResult{}, errors.New("short dns answer")
		}
		typ := binary.BigEndian.Uint16(packet[offset : offset+2])
		class := binary.BigEndian.Uint16(packet[offset+2 : offset+4])
		rdlen := int(binary.BigEndian.Uint16(packet[offset+8 : offset+10]))
		offset += 10
		if len(packet) < offset+rdlen {
			return DANEResult{}, errors.New("short dns rdata")
		}
		rdata := packet[offset : offset+rdlen]
		offset += rdlen

		if typ != dnsTypeTLSA || class != dnsClassIN {
			continue
		}
		if len(rdata) < 3 {
			continue
		}
		records = append(records, TLSARecord{
			Usage:                  rdata[0],
			Selector:               rdata[1],
			MatchingType:           rdata[2],
			CertificateAssociation: append([]byte(nil), rdata[3:]...),
		})
	}
	return DANEResult{AuthenticatedData: ad, Records: records}, nil
}

func skipDNSName(packet []byte, offset int) (int, error) {
	for {
		if offset >= len(packet) {
			return 0, errors.New("dns name overflow")
		}
		l := int(packet[offset])
		if l == 0 {
			return offset + 1, nil
		}
		if l&0xc0 == 0xc0 {
			if offset+1 >= len(packet) {
				return 0, errors.New("short dns compression pointer")
			}
			return offset + 2, nil
		}
		offset++
		if l > 63 || offset+l > len(packet) {
			return 0, errors.New("invalid dns label length")
		}
		offset += l
	}
}
