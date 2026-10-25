package config

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var tlsReportDomainPattern = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

type TLSReportConfig struct {
	Enabled        bool          `yaml:"enabled"`
	Organization   string        `yaml:"organization"`
	ContactInfo    string        `yaml:"contact_info"`
	ReportDomain   string        `yaml:"report_domain"`
	FlushInterval  time.Duration `yaml:"flush_interval"`
	MaxRecords     int           `yaml:"max_records"`
	MaxReportBytes int           `yaml:"max_report_bytes"`
}

func (c *TLSReportConfig) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Enabled        bool   `yaml:"enabled"`
		Organization   string `yaml:"organization"`
		ContactInfo    string `yaml:"contact_info"`
		ReportDomain   string `yaml:"report_domain"`
		FlushInterval  string `yaml:"flush_interval"`
		MaxRecords     int    `yaml:"max_records"`
		MaxReportBytes int    `yaml:"max_report_bytes"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*c = defaultTLSReportConfig()
	c.Enabled, c.Organization, c.ContactInfo, c.ReportDomain = raw.Enabled, raw.Organization, raw.ContactInfo, raw.ReportDomain
	if raw.FlushInterval != "" {
		value, err := time.ParseDuration(raw.FlushInterval)
		if err != nil {
			return err
		}
		c.FlushInterval = value
	}
	if raw.MaxRecords != 0 {
		c.MaxRecords = raw.MaxRecords
	}
	if raw.MaxReportBytes != 0 {
		c.MaxReportBytes = raw.MaxReportBytes
	}
	return nil
}

func defaultTLSReportConfig() TLSReportConfig {
	return TLSReportConfig{FlushInterval: time.Hour, MaxRecords: 100000, MaxReportBytes: 5 * 1024 * 1024}
}

func (c TLSReportConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	contact := strings.TrimSpace(c.ContactInfo)
	address, addressErr := mail.ParseAddress(contact)
	validContact := addressErr == nil && address.Address == contact && strings.Contains(contact, "@")
	if strings.TrimSpace(c.Organization) == "" || len(c.Organization) > 256 || len(contact) > 256 || !validContact || !tlsReportDomainPattern.MatchString(strings.TrimSuffix(strings.TrimSpace(c.ReportDomain), ".")) {
		return errors.New("tls_report requires organization, contact_info, and a valid report_domain")
	}
	if c.FlushInterval < time.Minute || c.FlushInterval > 24*time.Hour || c.MaxRecords < 1 || c.MaxRecords > 1000000 || c.MaxReportBytes < 1024 || c.MaxReportBytes > 25*1024*1024 {
		return errors.New("tls_report limits are out of range")
	}
	return nil
}
