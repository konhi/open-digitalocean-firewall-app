package ip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	testDetectedIP       = "203.0.113.42"
	testHealthyEndpoint  = "https://healthy.example"
	testIPv6Endpoint     = "https://ipv6.example"
	testFailingEndpoint  = "https://failing.example"
	testInvalidEndpoint  = "https://invalid.example"
	testUnreachEndpoint  = "https://unreachable.example"
	testBadGateEndpoint  = "https://bad-gateway.example"
)

var (
	errUnexpectedEndpoint = errors.New("unexpected endpoint")
	errConnectionRefused  = errors.New("connection refused")
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func newStubDetector(endpoints []string, responses map[string]func(*http.Request) (*http.Response, error)) *HTTPDetector {
	return &HTTPDetector{
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				handler, ok := responses[req.URL.String()]
				if !ok {
					return nil, fmt.Errorf("%w: %s", errUnexpectedEndpoint, req.URL.String())
				}
				return handler(req)
			}),
		},
		Endpoints: endpoints,
	}
}

func TestHTTPDetector_Detect_ReturnsTrimmedIPv4FromHealthyEndpoint(t *testing.T) {
	d := newStubDetector(
		[]string{testHealthyEndpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testHealthyEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, "  "+testDetectedIP+"\n"), nil
			},
		},
	)

	got, err := d.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() unexpected error: %v", err)
	}
	if got != testDetectedIP {
		t.Fatalf("Detect() = %q, want %q", got, testDetectedIP)
	}
}

func TestHTTPDetector_Detect_FallsBackWhenEndpointReturnsNon200Status(t *testing.T) {
	d := newStubDetector(
		[]string{testFailingEndpoint, testHealthyEndpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testFailingEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusInternalServerError, "error"), nil
			},
			testHealthyEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, testDetectedIP), nil
			},
		},
	)

	got, err := d.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() unexpected error: %v", err)
	}
	if got != testDetectedIP {
		t.Fatalf("Detect() = %q, want %q", got, testDetectedIP)
	}
}

func TestHTTPDetector_Detect_FallsBackWhenEndpointReturnsInvalidIP(t *testing.T) {
	d := newStubDetector(
		[]string{testInvalidEndpoint, testHealthyEndpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testInvalidEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, "not-an-ip"), nil
			},
			testHealthyEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, testDetectedIP), nil
			},
		},
	)

	got, err := d.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() unexpected error: %v", err)
	}
	if got != testDetectedIP {
		t.Fatalf("Detect() = %q, want %q", got, testDetectedIP)
	}
}

func TestHTTPDetector_Detect_RejectsIPv6AndFallsBackToIPv4Endpoint(t *testing.T) {
	d := newStubDetector(
		[]string{testIPv6Endpoint, testHealthyEndpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testIPv6Endpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, "2001:db8::1\n"), nil
			},
			testHealthyEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, testDetectedIP), nil
			},
		},
	)

	got, err := d.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() unexpected error: %v", err)
	}
	if got != testDetectedIP {
		t.Fatalf("Detect() = %q, want %q", got, testDetectedIP)
	}
}

func TestHTTPDetector_Detect_FallsBackOnTransportError(t *testing.T) {
	d := newStubDetector(
		[]string{testUnreachEndpoint, testHealthyEndpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testUnreachEndpoint: func(*http.Request) (*http.Response, error) {
				return nil, errConnectionRefused
			},
			testHealthyEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, testDetectedIP), nil
			},
		},
	)

	got, err := d.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() unexpected error: %v", err)
	}
	if got != testDetectedIP {
		t.Fatalf("Detect() = %q, want %q", got, testDetectedIP)
	}
}

func TestHTTPDetector_Detect_ReturnsErrorWhenAllEndpointsFail(t *testing.T) {
	d := newStubDetector(
		[]string{testBadGateEndpoint, testIPv6Endpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testBadGateEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusBadGateway, ""), nil
			},
			testIPv6Endpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, "2001:db8::1"), nil
			},
		},
	)

	_, err := d.Detect(context.Background())

	if !errors.Is(err, ErrUnexpectedStatus) || !errors.Is(err, ErrNotIPv4) {
		t.Fatalf("Detect() error = %v, want wrapped ErrUnexpectedStatus and ErrNotIPv4", err)
	}
}

func TestHTTPDetector_Detect_FailsImmediatelyWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := newStubDetector(
		[]string{testHealthyEndpoint},
		map[string]func(*http.Request) (*http.Response, error){
			testHealthyEndpoint: func(*http.Request) (*http.Response, error) {
				return httpResponse(http.StatusOK, testDetectedIP), nil
			},
		},
	)

	_, err := d.Detect(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Detect() error = %v, want context.Canceled", err)
	}
}
