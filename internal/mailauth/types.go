package mailauth

type Action string

const (
	ActionAccept     Action = "accept"
	ActionQuarantine Action = "quarantine"
	ActionReject     Action = "reject"
)

type Result struct {
	SPF         SPFResult
	SPFHelo     SPFResult
	SPFMailFrom SPFResult
	DKIM        DKIMResult
	DMARC       DMARCResult
	ARC         ARCResult
	Action      Action
	Reason      string
}

type SPFResult struct {
	Domain string
	Result string
	Reason string
}

type DKIMSigResult struct {
	Domain   string
	Selector string
	Result   string
	Reason   string
}

type DKIMResult struct {
	Result string
	Sigs   []DKIMSigResult
}

type DMARCResult struct {
	Engine          string
	LegacyResult    string
	LegacyPolicy    string
	Domain          string
	Result          string
	Policy          string
	SubdomainPolicy string
	Percent         int
	FailureOptions  []string
	ReportFormat    []string
	ReportInterval  int
	AggregateReport []string
	FailureReport   []string
	Reason          string
	// DMARCbis retains the RFC 9989 evaluation for comparison and reporting.
	DMARCbis *DMARCbisResult
}

type DMARCbisResult struct {
	Result          string
	AuthorDomain    string
	PolicyDomain    string
	OrgDomain       string
	Policy          string
	PublishedP      string
	PublishedSP     string
	PublishedNP     string
	FailureOptions  string
	DKIMMode        string
	SPFMode         string
	Testing         string
	SPFAligned      bool
	DKIMAligned     bool
	AggregateReport []string
	FailureReport   []string
	Discovery       string
	Reason          string
}

type ARCResult struct {
	Result string
	Reason string
}
