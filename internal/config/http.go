package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"
)

// HTTPConfig holds connection and session settings for the single-node API.
type HTTPConfig struct {
	ListenAddr     string
	PublicOrigin   string
	TLSCert        string
	TLSKey         string
	SessionTTL     time.Duration
	MaxLiveClients int
}

func (c HTTPConfig) validate(errs []error) []error {
	_, port, err := net.SplitHostPort(c.ListenAddr)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 0 || n > 65535 {
		errs = append(errs, errors.New("http_addr must be a host:port address"))
	}
	if c.PublicOrigin != "" {
		u, err := url.Parse(c.PublicOrigin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Hostname() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
			u.Fragment != "" || u.String() != c.PublicOrigin {
			errs = append(errs, errors.New("public_origin must be an HTTP(S) origin without a path, query, or fragment"))
		}
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		errs = append(errs, errors.New("tls_cert and tls_key must be configured together"))
	}
	if c.MaxLiveClients < 1 || c.MaxLiveClients > 64 {
		errs = append(errs, errors.New("max_live_clients must be in [1, 64]"))
	}
	return appendDurationBounds(errs, "session_ttl", c.SessionTTL, time.Minute, 7*24*time.Hour)
}
