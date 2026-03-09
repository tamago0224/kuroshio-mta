package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const (
	typeA    = 1
	typeMX   = 15
	typeTXT  = 16
	typeTLSA = 52
	classIN  = 1
)

type recordSet struct {
	A    []aRecord    `json:"a"`
	MX   []mxRecord   `json:"mx"`
	TXT  []txtRecord  `json:"txt"`
	TLSA []tlsaRecord `json:"tlsa"`
}

type aRecord struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	TTL  uint32 `json:"ttl"`
}

type mxRecord struct {
	Name       string `json:"name"`
	Preference uint16 `json:"preference"`
	Host       string `json:"host"`
	TTL        uint32 `json:"ttl"`
}

type txtRecord struct {
	Name  string   `json:"name"`
	Texts []string `json:"texts"`
	TTL   uint32   `json:"ttl"`
}

type tlsaRecord struct {
	Name         string `json:"name"`
	Usage        uint8  `json:"usage"`
	Selector     uint8  `json:"selector"`
	MatchingType uint8  `json:"matching_type"`
	DataHex      string `json:"data_hex"`
	TTL          uint32 `json:"ttl"`
}

type rr struct {
	name  string
	typ   uint16
	class uint16
	ttl   uint32
	rdata []byte
}

func main() {
	path := strings.TrimSpace(os.Getenv("DNSMOCK_RECORDS_FILE"))
	if path == "" {
		path = "/records.json"
	}
	records, err := loadRecords(path)
	if err != nil {
		log.Fatalf("load records: %v", err)
	}

	pc, err := net.ListenPacket("udp", ":53")
	if err != nil {
		log.Fatalf("listen udp 53: %v", err)
	}
	defer pc.Close()

	ln, err := net.Listen("tcp", ":53")
	if err != nil {
		log.Fatalf("listen tcp 53: %v", err)
	}
	defer ln.Close()

	go serveUDP(pc, records)
	go serveTCP(ln, records)

	log.Printf("dnsmock started, records=%s", path)
	select {}
}

func serveUDP(pc net.PacketConn, records recordSet) {
	buf := make([]byte, 4096)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			log.Printf("udp read error: %v", err)
			continue
		}
		resp, err := answerQuery(buf[:n], records)
		if err != nil {
			log.Printf("udp query error: %v", err)
			continue
		}
		if _, err := pc.WriteTo(resp, addr); err != nil {
			log.Printf("udp write error: %v", err)
		}
	}
}

func serveTCP(ln net.Listener, records recordSet) {
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("tcp accept error: %v", err)
			continue
		}
		go func(conn net.Conn) {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			lenBuf := make([]byte, 2)
			if _, err := conn.Read(lenBuf); err != nil {
				return
			}
			n := int(binary.BigEndian.Uint16(lenBuf))
			if n <= 0 || n > 4096 {
				return
			}
			buf := make([]byte, n)
			if _, err := conn.Read(buf); err != nil {
				return
			}
			resp, err := answerQuery(buf, records)
			if err != nil {
				return
			}
			outLen := make([]byte, 2)
			binary.BigEndian.PutUint16(outLen, uint16(len(resp)))
			_, _ = conn.Write(append(outLen, resp...))
		}(c)
	}
}

func loadRecords(path string) (recordSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return recordSet{}, err
	}
	var r recordSet
	if err := json.Unmarshal(b, &r); err != nil {
		return recordSet{}, err
	}
	return r, nil
}

func answerQuery(query []byte, records recordSet) ([]byte, error) {
	if len(query) < 12 {
		return nil, errors.New("short query")
	}
	id := binary.BigEndian.Uint16(query[0:2])
	flags := binary.BigEndian.Uint16(query[2:4])
	qdCount := binary.BigEndian.Uint16(query[4:6])
	if qdCount != 1 {
		return nil, errors.New("only qdcount=1 supported")
	}

	offset := 12
	qname, next, err := readName(query, offset)
	if err != nil {
		return nil, err
	}
	offset = next
	if len(query) < offset+4 {
		return nil, errors.New("short question tail")
	}
	qtype := binary.BigEndian.Uint16(query[offset : offset+2])
	qclass := binary.BigEndian.Uint16(query[offset+2 : offset+4])
	offset += 4

	if qclass != classIN {
		return nil, errors.New("unsupported qclass")
	}

	answers := lookupRR(records, qname, qtype)
	rcode := uint16(0)
	if len(answers) == 0 {
		rcode = 3
	}

	resp := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(resp[0:2], id)
	// QR=1, RA=1, AD=1 (for DANE integration tests)
	outFlags := uint16(0x8000 | 0x0080 | 0x0020)
	if flags&0x0100 != 0 {
		outFlags |= 0x0100 // copy RD
	}
	outFlags |= rcode
	binary.BigEndian.PutUint16(resp[2:4], outFlags)
	binary.BigEndian.PutUint16(resp[4:6], 1)
	binary.BigEndian.PutUint16(resp[6:8], uint16(len(answers)))
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)

	resp = append(resp, query[12:offset]...)
	for _, a := range answers {
		resp = append(resp, 0xc0, 0x0c) // name pointer
		tmp := make([]byte, 10)
		binary.BigEndian.PutUint16(tmp[0:2], a.typ)
		binary.BigEndian.PutUint16(tmp[2:4], a.class)
		binary.BigEndian.PutUint32(tmp[4:8], a.ttl)
		binary.BigEndian.PutUint16(tmp[8:10], uint16(len(a.rdata)))
		resp = append(resp, tmp...)
		resp = append(resp, a.rdata...)
	}
	return resp, nil
}

func readName(packet []byte, offset int) (string, int, error) {
	labels := make([]string, 0, 4)
	for {
		if offset >= len(packet) {
			return "", 0, errors.New("name overflow")
		}
		l := int(packet[offset])
		offset++
		if l == 0 {
			break
		}
		if l > 63 || offset+l > len(packet) {
			return "", 0, errors.New("invalid label length")
		}
		labels = append(labels, string(packet[offset:offset+l]))
		offset += l
	}
	return strings.ToLower(strings.Join(labels, ".")), offset, nil
}

func lookupRR(records recordSet, qname string, qtype uint16) []rr {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(qname)), ".")
	out := make([]rr, 0, 4)
	switch qtype {
	case typeA:
		for _, r := range records.A {
			if normalizeName(r.Name) != name {
				continue
			}
			ip := net.ParseIP(strings.TrimSpace(r.IP)).To4()
			if ip == nil {
				continue
			}
			ttl := r.TTL
			if ttl == 0 {
				ttl = 60
			}
			out = append(out, rr{name: name, typ: typeA, class: classIN, ttl: ttl, rdata: []byte(ip)})
		}
	case typeMX:
		for _, r := range records.MX {
			if normalizeName(r.Name) != name {
				continue
			}
			host := normalizeName(r.Host)
			hostWire, err := encodeName(host)
			if err != nil {
				continue
			}
			rd := make([]byte, 2, 64)
			binary.BigEndian.PutUint16(rd[0:2], r.Preference)
			rd = append(rd, hostWire...)
			ttl := r.TTL
			if ttl == 0 {
				ttl = 60
			}
			out = append(out, rr{name: name, typ: typeMX, class: classIN, ttl: ttl, rdata: rd})
		}
	case typeTXT:
		for _, r := range records.TXT {
			if normalizeName(r.Name) != name {
				continue
			}
			rd := make([]byte, 0, 256)
			for _, t := range r.Texts {
				tb := []byte(t)
				if len(tb) > 255 {
					tb = tb[:255]
				}
				rd = append(rd, byte(len(tb)))
				rd = append(rd, tb...)
			}
			ttl := r.TTL
			if ttl == 0 {
				ttl = 60
			}
			out = append(out, rr{name: name, typ: typeTXT, class: classIN, ttl: ttl, rdata: rd})
		}
	case typeTLSA:
		for _, r := range records.TLSA {
			if normalizeName(r.Name) != name {
				continue
			}
			data, err := hex.DecodeString(strings.TrimSpace(r.DataHex))
			if err != nil {
				continue
			}
			rd := []byte{r.Usage, r.Selector, r.MatchingType}
			rd = append(rd, data...)
			ttl := r.TTL
			if ttl == 0 {
				ttl = 60
			}
			out = append(out, rr{name: name, typ: typeTLSA, class: classIN, ttl: ttl, rdata: rd})
		}
	}
	return out
}

func normalizeName(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), ".")
}

func encodeName(name string) ([]byte, error) {
	labels := strings.Split(normalizeName(name), ".")
	if len(labels) == 0 {
		return nil, fmt.Errorf("empty name")
	}
	out := make([]byte, 0, 64)
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return nil, fmt.Errorf("invalid label")
		}
		out = append(out, byte(len(label)))
		out = append(out, []byte(label)...)
	}
	out = append(out, 0)
	return out, nil
}
