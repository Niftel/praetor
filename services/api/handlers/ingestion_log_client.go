package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ingestionLogClient is the security boundary for authenticated requests to
// ingestion. It owns URL construction and verifies the final request origin at
// the network sink so future callers cannot accidentally dispatch elsewhere.
type ingestionLogClient struct {
	baseURL       *url.URL
	httpClient    *http.Client
	internalToken string
}

func newIngestionLogClient(rawURL, internalToken string) (*ingestionLogClient, error) {
	baseURL, err := parseIngestionBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &ingestionLogClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		internalToken: internalToken,
	}, nil
}

func parseIngestionBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "http://ingestion:8081"
	}
	base, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse ingestion URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("ingestion URL scheme must be http or https")
	}
	if base.Hostname() == "" {
		return nil, fmt.Errorf("ingestion URL must include a host")
	}
	if base.User != nil {
		return nil, fmt.Errorf("ingestion URL must not include user information")
	}
	if base.Path != "" && base.Path != "/" {
		return nil, fmt.Errorf("ingestion URL must not include a path")
	}
	if base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, fmt.Errorf("ingestion URL must not include a query or fragment")
	}
	base.Path = ""
	return base, nil
}

func (c *ingestionLogClient) fetch(ctx context.Context, runID uuid.UUID, since string) (*http.Response, error) {
	req, err := c.newRequest(ctx, runID, since)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *ingestionLogClient) newRequest(ctx context.Context, runID uuid.UUID, since string) (*http.Request, error) {
	upstream := *c.baseURL
	upstream.Path = "/api/v1/runs/" + runID.String() + "/logs"
	query := upstream.Query()
	query.Set("since", since)
	upstream.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.internalToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.internalToken)
	}
	return req, nil
}

func (c *ingestionLogClient) do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("ingestion request URL is missing")
	}
	if req.URL.Scheme != c.baseURL.Scheme || req.URL.Host != c.baseURL.Host || req.URL.User != nil {
		return nil, fmt.Errorf("ingestion request origin %q does not match configured origin %q", req.URL.Redacted(), c.baseURL.Redacted())
	}
	// #nosec G704 -- scheme and host are checked against the startup-validated
	// origin immediately above, and this client refuses every redirect.
	return c.httpClient.Do(req)
}
