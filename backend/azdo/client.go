// Package azdo is a read-only client for the Azure DevOps REST API.
//
// It follows the product's "Azure DevOps is the source of truth" decision: the
// client retrieves and returns what Azure DevOps reports; it does not compute a
// competing effective-permission verdict. Authentication is delegated to the
// Azure CLI (a required, non-bundled prerequisite), which supplies an access
// token for the Azure DevOps resource.
package azdo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AzureDevOpsResourceID is the Microsoft Entra resource ID for Azure DevOps,
// used when requesting an access token for the service.
const AzureDevOpsResourceID = "499b84ac-1321-427f-aa17-267ca6975798"

// DefaultBaseURL is the Azure DevOps Services host for organization-scoped calls.
const DefaultBaseURL = "https://dev.azure.com"

// VSSPSBaseURL hosts the Graph and identity APIs, which are not exposed on the
// main dev.azure.com host. Graph endpoints return a 404 "controller not found"
// when called against DefaultBaseURL.
const VSSPSBaseURL = "https://vssps.dev.azure.com"

// HTTPClient executes HTTP requests. It is replaced in tests.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// TokenProvider yields a valid access token for the Azure DevOps resource.
type TokenProvider interface {
	// Token returns a non-empty access token. The ctx may be used for any
	// interactive/side-effectful acquisition the provider needs to perform.
	Token(ctx context.Context) (string, error)
}

// AzCLITokenProvider acquires tokens by running `az account get-access-token`.
type AzCLITokenProvider struct {
	// LookPath resolves the az executable (defaults to exec.LookPath).
	LookPath func(string) (string, error)
	// Run executes the az command (defaults to exec.CommandContext-compatible).
	Run func(ctx context.Context, azPath string, args ...string) ([]byte, error)
}

// NewAzCLITokenProvider returns a provider backed by the real az CLI.
func NewAzCLITokenProvider() *AzCLITokenProvider {
	return &AzCLITokenProvider{
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, azPath string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, azPath, args...).Output()
		},
	}
}

func (p *AzCLITokenProvider) Token(ctx context.Context) (string, error) {
	if p.LookPath == nil || p.Run == nil {
		return "", errors.New("azdo: az CLI provider is not configured")
	}
	path, err := p.LookPath("az")
	if err != nil {
		return "", fmt.Errorf("azdo: az CLI not found on PATH: %w", err)
	}
	out, err := p.Run(ctx, path,
		"account", "get-access-token",
		"--resource", AzureDevOpsResourceID,
		"--output", "json",
	)
	if err != nil {
		return "", fmt.Errorf("azdo: az get-access-token failed: %w", err)
	}
	var resp struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("azdo: parsing az token output: %w", err)
	}
	if resp.AccessToken == "" {
		return "", errors.New("azdo: az get-access-token returned no accessToken")
	}
	return resp.AccessToken, nil
}

// Client is a read-only Azure DevOps API client for one organization.
type Client struct {
	org        string
	baseURL    string
	vsspsURL   string
	httpClient HTTPClient
	token      TokenProvider
	authHeader string // "Bearer" by default; overridable in tests
	userAgent  string
	maxRetries int
	retryBase  time.Duration
	retryMax   time.Duration

	mu        sync.Mutex // guards cachedTok
	cachedTok string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP transport (used in tests).
func WithHTTPClient(h HTTPClient) Option { return func(c *Client) { c.httpClient = h } }

// WithBaseURL overrides the Azure DevOps host.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithVSSPSURL overrides the VSSPS (Graph/identity) host.
func WithVSSPSURL(u string) Option { return func(c *Client) { c.vsspsURL = strings.TrimRight(u, "/") } }

// WithTokenProvider overrides the token source.
func WithTokenProvider(t TokenProvider) Option { return func(c *Client) { c.token = t } }

// WithAuthHeader sets the auth scheme prefix (tests may use "Basic").
func WithAuthHeader(s string) Option { return func(c *Client) { c.authHeader = s } }

// WithRetry sets retry/backoff limits for throttled or transient calls.
func WithRetry(max int, base, hard time.Duration) Option {
	return func(c *Client) {
		c.maxRetries = max
		c.retryBase = base
		c.retryMax = hard
	}
}

// NewClient builds a Client for org. By default it authenticates via the
// Azure CLI and talks to dev.azure.com. Pass WithHTTPClient/WithBaseURL in
// tests.
func NewClient(org string, opts ...Option) (*Client, error) {
	if org == "" {
		return nil, errors.New("azdo: organization is required")
	}
	c := &Client{
		org:        org,
		baseURL:    DefaultBaseURL,
		vsspsURL:   VSSPSBaseURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		token:      NewAzCLITokenProvider(),
		authHeader: "Bearer",
		userAgent:  "azuredevops-permissions-visualiser/0.0.1",
		maxRetries: 3,
		retryBase:  500 * time.Millisecond,
		retryMax:   8 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func (c *Client) tokenFor(ctx context.Context) (string, error) {
	if c.token == nil {
		return "", errors.New("azdo: no token provider configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedTok != "" {
		return c.cachedTok, nil
	}
	tok, err := c.token.Token(ctx)
	if err != nil {
		return "", err
	}
	c.cachedTok = tok
	return tok, nil
}

// request builds an authenticated GET or POST request against the org URL.
// host is one of baseURL or vsspsURL.
func (c *Client) request(ctx context.Context, method, host, apiPath string, query url.Values, body any) (*http.Request, error) {
	tok, err := c.tokenFor(ctx)
	if err != nil {
		return nil, err
	}
	u := host + "/" + c.org + apiPath
	if query != nil {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader+" "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	// Azure DevOps passes an Entra access token through for Microsoft
	// (personal) accounts only when this header is set; without it dev.azure.com
	// redirects to its sign-in page with a 3xx even when the token is valid.
	// The Azure CLI's azure-devops extension sends it unconditionally; mirror that.
	req.Header.Set("X-VSS-ForceMsaPassThrough", "true")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// do executes a request with retry/backoff on transient statuses.
func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastErr error
	delay := c.retryBase
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("azdo: request failed: %w", err)
		} else if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("azdo: status %d (attempt %d)", resp.StatusCode, attempt+1)
			if attempt < c.maxRetries {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				delay *= 2
				if delay > c.retryMax {
					delay = c.retryMax
				}
				continue
			}
		} else {
			return resp, nil
		}
	}
	return nil, lastErr
}

// get executes an authenticated GET against the org host and decodes JSON.
func (c *Client) get(ctx context.Context, apiPath string, query url.Values, out any) error {
	return c.getOn(ctx, c.baseURL, apiPath, query, out)
}

// vsspsGet executes an authenticated GET against the VSSPS host (Graph and
// identity APIs, which are not exposed on the org host) and decodes JSON.
func (c *Client) vsspsGet(ctx context.Context, apiPath string, query url.Values, out any) error {
	return c.getOn(ctx, c.vsspsURL, apiPath, query, out)
}

func (c *Client) getOn(ctx context.Context, host, apiPath string, query url.Values, out any) error {
	req, err := c.request(ctx, http.MethodGet, host, apiPath, query, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		// Azure DevOps redirects to its sign-in page when it cannot authenticate
		// the caller (e.g. expired/invalid token or no access to the org). The
		// body is HTML, so never try to decode it as JSON.
		msg := "authentication required (Azure DevOps returned a redirect)"
		if strings.Contains(resp.Header.Get("Location"), "_signin") {
			msg = "authentication required (Azure DevOps redirected to its sign-in page)"
		}
		return fmt.Errorf("azdo: HTTP %d: %s; verify you are signed in with `az login` and can access the organization", resp.StatusCode, msg)
	case resp.StatusCode >= 400:
		return c.decodeError(resp)
	}
	if err := ensureJSON(resp); err != nil {
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ensureJSON guards against a success-status response that is actually an HTML
// page (for example a sign-in or error page) instead of JSON.
func ensureJSON(resp *http.Response) error {
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return errors.New("azdo: Azure DevOps returned an HTML page instead of JSON (authentication or authorization problem)")
	}
	return nil
}

// decodeError builds a descriptive error from a non-2xx response.
func (c *Client) decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var apiErr struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &apiErr)
	msg := strings.TrimSpace(apiErr.Message)
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = resp.Status
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	// Azure DevOps can return an HTML page for authorization failures.
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") ||
		strings.HasPrefix(strings.TrimSpace(string(body)), "<") {
		return fmt.Errorf("azdo: HTTP %d: Azure DevOps returned an HTML error page (authentication or authorization problem); verify `az login` and organization access", resp.StatusCode)
	}
	return fmt.Errorf("azdo: HTTP %d: %s", resp.StatusCode, msg)
}

// intQuery sets an integer query parameter.
func intQuery(v url.Values, key string, n int) {
	v.Set(key, strconv.Itoa(n))
}

// apiVersion returns a query value for a given REST version.
func apiVersion(v string) url.Values {
	return url.Values{"api-version": {v}}
}
