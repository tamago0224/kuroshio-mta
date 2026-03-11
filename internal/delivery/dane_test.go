package delivery

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDANEResultHasUsableTLSA(t *testing.T) {
	cases := []struct {
		name   string
		record TLSARecord
		want   bool
	}{
		{
			name:   "3-1-1 (DANE-EE SPKI SHA-256)",
			record: TLSARecord{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0xaa}},
			want:   true,
		},
		{
			name:   "3-0-1 (DANE-EE Cert SHA-256)",
			record: TLSARecord{Usage: 3, Selector: 0, MatchingType: 1, CertificateAssociation: []byte{0xaa}},
			want:   true,
		},
		{
			name:   "2-0-1 (DANE-TA Cert SHA-256)",
			record: TLSARecord{Usage: 2, Selector: 0, MatchingType: 1, CertificateAssociation: []byte{0xaa}},
			want:   true,
		},
		{
			name:   "2-1-1 (DANE-TA SPKI SHA-256)",
			record: TLSARecord{Usage: 2, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0xaa}},
			want:   true,
		},
		{
			name:   "unsupported matching type",
			record: TLSARecord{Usage: 3, Selector: 1, MatchingType: 0, CertificateAssociation: []byte{0xaa}},
			want:   false,
		},
		{
			name:   "unsupported usage",
			record: TLSARecord{Usage: 1, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0xaa}},
			want:   false,
		},
		{
			name:   "empty association",
			record: TLSARecord{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: nil},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DANEResult{
				AuthenticatedData: true,
				Records:           []TLSARecord{tc.record},
			}
			got := res.HasUsableTLSA()
			if got != tc.want {
				t.Fatalf("HasUsableTLSA()=%v want=%v record=%+v", got, tc.want, tc.record)
			}
			if (DANEResult{AuthenticatedData: false, Records: []TLSARecord{tc.record}}).HasUsableTLSA() {
				t.Fatal("dnssec ad=false must not be treated as usable")
			}
		})
	}
}

func TestDANEResultHasUsableTLSAWithTrustModel(t *testing.T) {
	rec := TLSARecord{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0xaa}}

	adRequired := DANEResult{AuthenticatedData: false, Records: []TLSARecord{rec}}
	if adRequired.HasUsableTLSAWithTrustModel("ad_required") {
		t.Fatal("ad_required must reject AD=false")
	}

	insecureAllowed := DANEResult{AuthenticatedData: false, Records: []TLSARecord{rec}}
	if !insecureAllowed.HasUsableTLSAWithTrustModel("insecure_allow_unsigned") {
		t.Fatal("insecure_allow_unsigned should allow AD=false when profile is otherwise usable")
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

func TestDANEResolverLookupHost_FollowsCNAMEForTLSA(t *testing.T) {
	var lookedUp []string
	r := NewDANEResolver(time.Second, func(_ context.Context, host string, _ int) (DANEResult, error) {
		lookedUp = append(lookedUp, host)
		if host == "mx.real.example.net" {
			return DANEResult{
				AuthenticatedData: true,
				Records:           []TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0xaa}}},
			}, nil
		}
		return DANEResult{}, nil
	})
	r.lookupCNAMEFn = func(_ context.Context, host string) (string, error) {
		if host == "mx.alias.example.net" {
			return "mx.real.example.net.", nil
		}
		return "", errors.New("no cname")
	}

	res, err := r.LookupHost(context.Background(), "mx.alias.example.net", 25)
	if err != nil {
		t.Fatalf("LookupHost: %v", err)
	}
	if !res.HasUsableTLSA() {
		t.Fatalf("expected usable TLSA through CNAME, got %+v", res)
	}
	wantHosts := []string{"mx.alias.example.net", "mx.real.example.net"}
	if !reflect.DeepEqual(lookedUp, wantHosts) {
		t.Fatalf("lookup hosts mismatch got=%v want=%v", lookedUp, wantHosts)
	}
}

func TestDANEResolverLookupHost_CNAMEChainLoopStops(t *testing.T) {
	var lookedUp []string
	r := NewDANEResolver(time.Second, func(_ context.Context, host string, _ int) (DANEResult, error) {
		lookedUp = append(lookedUp, host)
		return DANEResult{}, nil
	})
	r.lookupCNAMEFn = func(_ context.Context, host string) (string, error) {
		switch host {
		case "mx1.example.net":
			return "mx2.example.net.", nil
		case "mx2.example.net":
			return "mx1.example.net.", nil
		default:
			return "", errors.New("no cname")
		}
	}

	_, err := r.LookupHost(context.Background(), "mx1.example.net", 25)
	if err != nil {
		t.Fatalf("LookupHost should stop loop and return no-record result: %v", err)
	}
	if len(lookedUp) > 2 {
		t.Fatalf("expected loop detection to stop exploration, looked up=%v", lookedUp)
	}
}
