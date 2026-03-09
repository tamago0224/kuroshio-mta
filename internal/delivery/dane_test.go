package delivery

import (
	"encoding/binary"
	"testing"
)

func TestDANEResultHasUsableTLSA(t *testing.T) {
	ok := DANEResult{
		AuthenticatedData: true,
		Records:           []TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0xaa}}},
	}
	if !ok.HasUsableTLSA() {
		t.Fatal("expected usable tlsa")
	}
	if (DANEResult{AuthenticatedData: false, Records: ok.Records}).HasUsableTLSA() {
		t.Fatal("dnssec ad=false must not be treated as usable")
	}
}

func TestParseTLSAResponse(t *testing.T) {
	queryID := uint16(0x1234)
	packet := []byte{
		0x12, 0x34, // id
		0x81, 0xa0, // flags: response + RD + RA + AD
		0x00, 0x01, // qdcount
		0x00, 0x01, // ancount
		0x00, 0x00, // nscount
		0x00, 0x00, // arcount
		0x03, '_', '2', '5', // _25
		0x04, '_', 't', 'c', 'p',
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'n', 'e', 't',
		0x00,
		0x00, 0x34, // QTYPE TLSA
		0x00, 0x01, // QCLASS IN
		0xc0, 0x0c, // NAME pointer
		0x00, 0x34, // TYPE TLSA
		0x00, 0x01, // CLASS IN
		0x00, 0x00, 0x01, 0x2c, // TTL
		0x00, 0x05, // RDLENGTH
		0x03,       // usage
		0x01,       // selector
		0x01,       // matching
		0xde, 0xad, // cert association
	}
	res, err := parseTLSAResponse(packet, queryID)
	if err != nil {
		t.Fatalf("parseTLSAResponse: %v", err)
	}
	if !res.AuthenticatedData {
		t.Fatal("expected AD=true")
	}
	if len(res.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(res.Records))
	}
	r := res.Records[0]
	if r.Usage != 3 || r.Selector != 1 || r.MatchingType != 1 {
		t.Fatalf("unexpected parsed record: %+v", r)
	}
	if binary.BigEndian.Uint16(r.CertificateAssociation) != 0xdead {
		t.Fatalf("unexpected cert association: %x", r.CertificateAssociation)
	}
}
