package smtp

import (
	"errors"

	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

func (s *Server) signingIdentity(ss *session) (*model.SigningIdentity, error) {
	if s.cfg.DKIMProfilesFile == "" {
		return nil, nil
	}
	identity := &model.SigningIdentity{}
	if !s.submission || !ss.authOK {
		return identity, nil
	}
	author, err := dkim.FromAddress(ss.data)
	if err != nil || !senderAllowedForAuth(ss.auth, ss.mailFrom) || !senderAllowedForAuth(ss.auth, author) {
		return nil, errors.New("author is outside authenticated sender scope")
	}
	fromDomain, _ := util.DomainOf(author)
	envelopeDomain, _ := util.DomainOf(ss.mailFrom)
	if fromDomain != envelopeDomain {
		return nil, errors.New("author and envelope signing domains differ")
	}
	identity.Domain = fromDomain
	return identity, nil
}
