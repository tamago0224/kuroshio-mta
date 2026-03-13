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
	Domain           string
	Result           string
	Policy           string
	SubdomainPolicy  string
	Percent          int
	FailureOptions   []string
	ReportFormat     []string
	ReportInterval   int
	AggregateReport  []string
	FailureReport    []string
	Reason           string
}

type ARCResult struct {
	Result string
	Reason string
}
