package delivery

import (
	"context"
	"errors"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

func (c *Client) ReloadSigning(ctx context.Context) (bool, error) {
	if c.profiles == nil {
		return false, nil
	}
	return c.profiles.Reload(ctx)
}

func (c *Client) prepareOutboundMessage(ctx context.Context, msg *model.Message) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.profiles == nil {
		return c.prepareOutboundData(msg.Data)
	}
	if msg.SigningIdentity == nil {
		// Existing automatic DSNs/reports have a null reverse-path and no identity.
		if msg.MailFrom == "" {
			return msg.Data, nil
		}
		return nil, errors.New("message has no trusted signing identity; migrate legacy queue before enabling profiles")
	}
	domain := msg.SigningIdentity.Domain
	if domain == "" {
		return msg.Data, nil
	}
	author, err := dkim.FromAddress(msg.Data)
	if err != nil {
		return nil, err
	}
	authorDomain, _ := util.DomainOf(author)
	envelopeDomain, _ := util.DomainOf(msg.MailFrom)
	if domain != strings.ToLower(domain) || domain != authorDomain || (msg.MailFrom != "" && domain != envelopeDomain) {
		return nil, errors.New("persisted signing identity does not match message")
	}
	pair, err := c.profiles.Signers(domain)
	if err != nil {
		return nil, err
	}
	if pair.DKIM == nil {
		return msg.Data, nil
	}
	raw, err := pair.DKIM.Sign(msg.Data)
	if err != nil {
		return nil, err
	}
	if pair.ARC != nil {
		return (&validatedARCSigner{signer: pair.ARC}).Sign(raw)
	}
	return raw, nil
}
