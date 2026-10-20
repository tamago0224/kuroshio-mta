package dmarcreport

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

func TestBuildXMLUsesRFC9990NamespaceOrderAndDomainOnlyIdentifiers(t *testing.T) {
	period := time.Date(2026, 10, 19, 16, 30, 0, 0, time.FixedZone("JST", 9*60*60))
	ob := testObservation(period)
	rows := []Aggregate{{Observation: ob, Count: 4}}
	got, err := BuildXML("Receiver", "postmaster@receiver.test", "stable-id", period, ob.Policy, ob.Discovery, rows)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`xmlns="`+Namespace+`"`)) {
		t.Fatalf("missing namespace: %s", got)
	}
	for _, part := range []string{"<version>1.0</version>", "<discovery_method>treewalk</discovery_method>", "<source_ip>192.0.2.7</source_ip>", "<count>4</count>", "<header_from>mail.example.test</header_from>", "<scope>mfrom</scope>"} {
		if !bytes.Contains(got, []byte(part)) {
			t.Errorf("missing %q", part)
		}
	}
	if strings.Contains(string(got), "message-id") || strings.Contains(string(got), "recipient@example") {
		t.Fatal("report includes message or recipient data")
	}
	decoder := xml.NewDecoder(bytes.NewReader(got))
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "feedback" {
			rootSeen = true
			if start.Name.Space != Namespace {
				t.Fatalf("namespace=%q", start.Name.Space)
			}
		}
	}
	if !rootSeen {
		t.Fatal("feedback root missing")
	}
}

func TestBuildXMLRejectsMixedPolicyAndEmptyRows(t *testing.T) {
	ob := testObservation(time.Now())
	if _, err := BuildXML("org", "contact", "id", time.Now(), ob.Policy, ob.Discovery, nil); err == nil {
		t.Fatal("accepted no records")
	}
	other := ob
	other.PolicyDomain = "other.test"
	if _, err := BuildXML("org", "contact", "id", time.Now(), ob.Policy, ob.Discovery, []Aggregate{{Observation: ob, Count: 1}, {Observation: other, Count: 1}}); err == nil {
		t.Fatal("accepted mixed policy domains")
	}
}

func TestGzipXMLRoundTrip(t *testing.T) {
	want := []byte("<feedback>report</feedback>")
	compressed, err := GzipXML(want)
	if err != nil {
		t.Fatal(err)
	}
	r, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
