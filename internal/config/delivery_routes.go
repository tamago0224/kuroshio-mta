package config

import (
	"errors"
	"fmt"
	"strings"
)

// DeliveryRoute selects a transport for exact domains or their subdomains.
// Routes are evaluated in configuration order.
type DeliveryRoute struct {
	Name              string   `yaml:"name"`
	Domains           []string `yaml:"domains"`
	Mode              string   `yaml:"mode"`
	RelayHost         string   `yaml:"relay_host,omitempty"`
	RelayPort         int      `yaml:"relay_port,omitempty"`
	RelayRequireTLS   bool     `yaml:"relay_require_tls,omitempty"`
	RelayUsername     string   `yaml:"relay_username,omitempty"`
	RelayPasswordFile string   `yaml:"relay_password_file,omitempty"`
	LMTPUnixSocket    string   `yaml:"lmtp_unix_socket,omitempty"`
}

func (c Config) validateDeliveryRoutes() error {
	names := make(map[string]struct{}, len(c.DeliveryRoutes))
	for i, route := range c.DeliveryRoutes {
		name := strings.TrimSpace(route.Name)
		if name == "" {
			return fmt.Errorf("delivery_routes[%d].name is required", i)
		}
		if _, exists := names[name]; exists {
			return fmt.Errorf("duplicate delivery route %q", name)
		}
		names[name] = struct{}{}
		if len(route.Domains) == 0 {
			return fmt.Errorf("delivery route %q requires domains", name)
		}
		for _, domain := range route.Domains {
			domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
			if !validLocalDomain(domain) {
				return fmt.Errorf("delivery route %q has invalid domain", name)
			}
		}
		switch strings.ToLower(strings.TrimSpace(route.Mode)) {
		case "mx", "local_spool":
			if route.RelayHost != "" || route.RelayPort != 0 || route.RelayRequireTLS || route.RelayUsername != "" || route.RelayPasswordFile != "" || route.LMTPUnixSocket != "" {
				return fmt.Errorf("delivery route %q has transport settings for incompatible mode", name)
			}
		case "relay":
			if strings.TrimSpace(route.RelayHost) == "" {
				return fmt.Errorf("delivery route %q requires relay_host", name)
			}
			if route.RelayPort < 0 || route.RelayPort > 65535 {
				return fmt.Errorf("delivery route %q has invalid relay_port", name)
			}
			credentials := route.RelayUsername != "" || route.RelayPasswordFile != ""
			if credentials && (route.RelayUsername == "" || route.RelayPasswordFile == "") {
				return fmt.Errorf("delivery route %q requires both relay_username and relay_password_file", name)
			}
			if credentials && !route.RelayRequireTLS {
				return errors.New("authenticated relay routes require relay_require_tls")
			}
			if route.LMTPUnixSocket != "" {
				return fmt.Errorf("delivery route %q has LMTP settings for relay mode", name)
			}
		case "lmtp":
			if strings.TrimSpace(route.LMTPUnixSocket) == "" {
				return fmt.Errorf("delivery route %q requires lmtp_unix_socket", name)
			}
			if route.RelayHost != "" || route.RelayPort != 0 || route.RelayRequireTLS || route.RelayUsername != "" || route.RelayPasswordFile != "" {
				return fmt.Errorf("delivery route %q has relay settings for LMTP mode", name)
			}
		default:
			return fmt.Errorf("delivery route %q has invalid mode", name)
		}
	}
	return nil
}
