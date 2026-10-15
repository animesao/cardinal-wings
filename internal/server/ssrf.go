package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ssrf.go guards every place where wings fetches a caller-supplied URL
// (container file pull, backup restore from URL). The endpoints are
// admin-only, but a stolen admin key must not become a stepping stone into
// the node's LAN or the cloud metadata service.
//
// Policy: only http/https, no credentials in the URL. Host IPs (after DNS)
// must not be loopback, link-local (covers 169.254.169.254 metadata),
// multicast, unspecified or otherwise reserved. RFC 1918 / LAN addresses
// stay allowed on purpose: node-to-node backup transfers legitimately run
// over private networks. Redirects are followed (mod CDNs redirect) but
// every hop is re-validated. DNS-rebinding between check and connect is a
// documented residual; the endpoints require an admin key.

// fetchTransport bounds handshake time; the caller's context bounds the body.
var fetchTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   30 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
	ExpectContinueTimeout: 10 * time.Second,
}

// validateFetchURL parses rawURL and rejects anything outside the fetch
// policy. It returns the parsed URL for the caller to log.
func validateFetchURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url required")
	}
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http(s) urls are allowed")
	}
	if u.User != nil {
		return nil, fmt.Errorf("credentials in url are not allowed")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("url has no host")
	}
	if err := validateFetchHost(u.Hostname()); err != nil {
		return nil, err
	}
	return u, nil
}

// validateFetchHost resolves the host and rejects dangerous ranges.
func validateFetchHost(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		return checkFetchIP(ip)
	}
	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("cannot resolve %q", host)
	}
	for _, ip := range ips {
		if err := checkFetchIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// checkFetchIP rejects loopback, link-local (cloud metadata), multicast,
// unspecified and other reserved ranges. Private LAN ranges pass: cluster
// transfers run over them.
func checkFetchIP(ip net.IP) error {
	if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("url resolves to a forbidden address")
	}
	return nil
}

// fetchGet performs a GET with redirect re-validation. maxBody caps the
// readable body; the caller wraps resp.Body with io.LimitReader as well.
func fetchGet(ctx context.Context, rawURL string) (*http.Response, error) {
	u, err := validateFetchURL(rawURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: fetchTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if _, err := validateFetchURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return resp, nil
}

// validatePanelURL checks a panel-pushed URL (POST /v1/config, static
// [panel] config): http/https with a host, no credentials. Panel links may
// point at LAN panels, so IP ranges are not restricted here — but a link
// that fails this check is never persisted, which stops typo/attacker URLs
// from diverting SFTP credential checks and backup callbacks.
func validatePanelURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid panel url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("panel url must be http(s)")
	}
	if u.User != nil {
		return fmt.Errorf("credentials in panel url are not allowed")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("panel url has no host")
	}
	return nil
}
