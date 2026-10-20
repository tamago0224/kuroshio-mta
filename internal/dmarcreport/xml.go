package dmarcreport

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"time"
)

const Namespace = "urn:ietf:params:xml:ns:dmarc-2.0"

type feedback struct {
	XMLName  xml.Name        `xml:"urn:ietf:params:xml:ns:dmarc-2.0 feedback"`
	Version  string          `xml:"version,omitempty"`
	Metadata reportMetadata  `xml:"report_metadata"`
	Policy   policyPublished `xml:"policy_published"`
	Records  []reportRecord  `xml:"record"`
}
type reportMetadata struct {
	OrgName   string    `xml:"org_name"`
	Email     string    `xml:"email"`
	ReportID  string    `xml:"report_id"`
	Range     dateRange `xml:"date_range"`
	Generator string    `xml:"generator,omitempty"`
}
type dateRange struct {
	Begin int64 `xml:"begin"`
	End   int64 `xml:"end"`
}
type policyPublished struct {
	Domain    string `xml:"domain"`
	Discovery string `xml:"discovery_method,omitempty"`
	P         string `xml:"p"`
	SP        string `xml:"sp,omitempty"`
	NP        string `xml:"np,omitempty"`
	FO        string `xml:"fo,omitempty"`
	ADKIM     string `xml:"adkim,omitempty"`
	ASPF      string `xml:"aspf,omitempty"`
	Testing   string `xml:"testing,omitempty"`
}
type reportRecord struct {
	Row         reportRow         `xml:"row"`
	Identifiers reportIdentifiers `xml:"identifiers"`
	AuthResults reportAuthResults `xml:"auth_results"`
}
type reportRow struct {
	SourceIP  string          `xml:"source_ip"`
	Count     uint64          `xml:"count"`
	Evaluated policyEvaluated `xml:"policy_evaluated"`
}
type policyEvaluated struct {
	Disposition string `xml:"disposition"`
	DKIM        string `xml:"dkim"`
	SPF         string `xml:"spf"`
}
type reportIdentifiers struct {
	HeaderFrom   string `xml:"header_from"`
	EnvelopeFrom string `xml:"envelope_from,omitempty"`
	EnvelopeTo   string `xml:"envelope_to,omitempty"`
}
type reportAuthResults struct {
	DKIM []reportDKIM `xml:"dkim,omitempty"`
	SPF  *reportSPF   `xml:"spf,omitempty"`
}
type reportDKIM struct {
	Domain   string `xml:"domain"`
	Selector string `xml:"selector"`
	Result   string `xml:"result"`
}
type reportSPF struct {
	Domain string `xml:"domain"`
	Scope  string `xml:"scope,omitempty"`
	Result string `xml:"result"`
}

func BuildXML(orgName, contact, reportID string, period time.Time, policy Policy, discovery string, rows []Aggregate) ([]byte, error) {
	if orgName == "" || contact == "" || reportID == "" || period.IsZero() || len(rows) == 0 {
		return nil, errors.New("incomplete DMARC report metadata or rows")
	}
	period = period.UTC().Truncate(24 * time.Hour)
	root := feedback{Version: "1.0", Metadata: reportMetadata{OrgName: orgName, Email: contact, ReportID: reportID, Range: dateRange{Begin: period.Unix(), End: period.Add(24*time.Hour).Unix() - 1}, Generator: "kuroshio-mta"}, Policy: policyPublished{Domain: rows[0].Observation.PolicyDomain, Discovery: discovery, P: policy.P, SP: policy.SP, NP: policy.NP, FO: policy.FO, ADKIM: policy.ADKIM, ASPF: policy.ASPF, Testing: policy.Testing}}
	for _, aggregate := range rows {
		ob := aggregate.Observation
		if aggregate.Count == 0 || ob.PolicyDomain != root.Policy.Domain || ob.Policy != policy || ob.Discovery != discovery {
			return nil, errors.New("DMARC report rows span policy domains or configurations")
		}
		record := reportRecord{Row: reportRow{SourceIP: ob.SourceIP, Count: aggregate.Count, Evaluated: policyEvaluated{Disposition: ob.Disposition, DKIM: ob.DKIMAligned, SPF: ob.SPFAligned}}, Identifiers: reportIdentifiers{HeaderFrom: ob.AuthorDomain, EnvelopeFrom: ob.EnvelopeFrom, EnvelopeTo: ob.EnvelopeTo}}
		for _, signature := range ob.DKIMResults {
			record.AuthResults.DKIM = append(record.AuthResults.DKIM, reportDKIM{Domain: signature.Domain, Selector: signature.Selector, Result: signature.Result})
		}
		if ob.SPFDomain != "" || ob.SPFResult != "" {
			record.AuthResults.SPF = &reportSPF{Domain: ob.SPFDomain, Scope: "mfrom", Result: ob.SPFResult}
		}
		root.Records = append(root.Records, record)
	}
	encoded, err := xml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode DMARC aggregate XML: %w", err)
	}
	return append([]byte(xml.Header), append(encoded, '\n')...), nil
}

func GzipXML(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
