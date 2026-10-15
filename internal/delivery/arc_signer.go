package delivery

import (
	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
)

// validatedARCSigner obtains the chain status from cryptographic validation,
// rather than trusting Authentication-Results supplied with the message.
type validatedARCSigner struct {
	signer *dkim.ARCFileSigner
}

func (s *validatedARCSigner) Sign(raw []byte) ([]byte, error) {
	headerPart, body, err := mailauth.SplitMessage(raw)
	if err != nil {
		return nil, err
	}
	headers, err := mailauth.ParseHeaders(headerPart)
	if err != nil {
		return nil, err
	}
	result := mailauth.EvalARC(headers, body)
	return s.signer.SignWithChainValidation(raw, result.Result)
}
