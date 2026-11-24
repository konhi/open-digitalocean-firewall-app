package ip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var (
	ErrUnexpectedStatus = errors.New("unexpected HTTP status")
	ErrNotIPv4          = errors.New("expected IPv4 address")
)

// DefaultEndpoints are public IPv4 echo services queried in order until one succeeds.
var DefaultEndpoints = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
}

// Detector resolves the device's current public IPv4 address.
type Detector interface {
	Detect(ctx context.Context) (string, error)
}

type HTTPDetector struct {
	Client    *http.Client
	Endpoints []string
}

func NewDetector() *HTTPDetector {
	return &HTTPDetector{
		Client: &http.Client{
			Timeout: 5 * time.Second,
		},
		Endpoints: DefaultEndpoints,
	}
}

// Detect tries each configured endpoint in order until one returns a valid IPv4 address.
func (d *HTTPDetector) Detect(ctx context.Context) (string, error) {
	endpoints := d.Endpoints
	if len(endpoints) == 0 {
		endpoints = DefaultEndpoints
	}

	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	var errs []error
	for _, endpoint := range endpoints {
		addr, err := d.fetch(ctx, client, endpoint)
		if err == nil {
			return addr, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", endpoint, err))
	}

	return "", fmt.Errorf("failed to detect public IPv4 address: %w", errors.Join(errs...))
}

func (d *HTTPDetector) fetch(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Open-DigitalOcean-Firewall-App")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}

	raw := strings.TrimSpace(string(body))
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", fmt.Errorf("invalid IP response %q: %w", raw, err)
	}
	if !addr.Is4() {
		return "", fmt.Errorf("%w: got %q", ErrNotIPv4, raw)
	}

	return addr.String(), nil
}
