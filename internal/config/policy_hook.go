package config

import (
	"errors"
	"net"
	"net/url"
	"time"
)

type PolicyHookConfig struct {
	URL               string        `yaml:"url"`
	Timeout           time.Duration `yaml:"timeout"`
	MaxConcurrent     int           `yaml:"max_concurrent"`
	MaxResponseBytes  int64         `yaml:"max_response_bytes"`
	SendBody          bool          `yaml:"send_body"`
	TokenFile         string        `yaml:"token_file"`
	AllowHTTPLoopback bool          `yaml:"allow_http_loopback"`
}

func (c PolicyHookConfig) Validate() error {
	if c.URL == "" {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("policy_hook.url must be an absolute endpoint without credentials, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !c.AllowHTTPLoopback || ip == nil || !ip.IsLoopback() {
			return errors.New("policy_hook.url requires HTTPS; explicit HTTP allowance is limited to literal loopback IPs")
		}
	}
	if c.Timeout < 0 || c.Timeout > 30*time.Second || c.MaxConcurrent < 0 || c.MaxConcurrent > 1000 || c.MaxResponseBytes < 0 || c.MaxResponseBytes > 1024*1024 {
		return errors.New("policy_hook limits out of range (timeout <=30s, concurrency <=1000, response <=1MiB)")
	}
	return nil
}
