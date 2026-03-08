package auth

type Action string

const (
	ActionAccept     Action = "accept"
	ActionQuarantine Action = "quarantine"
	ActionReject     Action = "reject"
)

type Result struct {
	SPF    SPFResult
	DKIM   DKIMResult
	DMARC  DMARCResult
	ARC    ARCResult
	Action Action
	Reason string
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
	Domain string
	Result string
	Policy string
	Reason string
}

type ARCResult struct {
	Result string
	Reason string
}
