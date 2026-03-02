// =============================================================================
// x402 External HTTP Client
// =============================================================================
// Makes direct HTTP requests to external x402-protected APIs.
// The Botwallet server NEVER proxies API data — this client fetches directly.
//
// Security:
//   - Rejects private IPs, localhost, and non-HTTP(S) schemes
//   - Paid requests (FetchWithPayment) enforce HTTPS — signed transactions
//     must never travel over plaintext
//   - Timeouts enforced to prevent hanging on slow APIs
//   - Response body size capped to prevent OOM
// =============================================================================

package x402

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxResponseBody = 10 * 1024 * 1024 // 10 MB cap
	requestTimeout  = 30 * time.Second
)

// ValidateURL rejects URLs that point to private/internal networks.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("only http/https URLs are allowed, got %q", u.Scheme)
	}

	hostname := u.Hostname()

	if hostname == "localhost" || hostname == "" {
		return fmt.Errorf("localhost URLs are not allowed")
	}

	ip := net.ParseIP(hostname)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("private/internal IP addresses are not allowed: %s", hostname)
		}
	}

	return nil
}

// Fetch makes a direct HTTP request to an external API without any payment header.
func Fetch(targetURL, method string, headers map[string]string, body string) (*http.Response, error) {
	if err := ValidateURL(targetURL); err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(strings.ToUpper(method), targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Botwallet-CLI/x402")

	client := &http.Client{Timeout: requestTimeout}
	return client.Do(req)
}

// FetchWithPayment retries the request with the X-Payment header attached.
// Enforces HTTPS — a signed Solana transaction must never travel over plaintext.
func FetchWithPayment(targetURL, method string, headers map[string]string, body string, xPayment string) (*http.Response, error) {
	if err := ValidateURL(targetURL); err != nil {
		return nil, err
	}

	u, _ := url.Parse(targetURL)
	if strings.ToLower(u.Scheme) != "https" {
		return nil, fmt.Errorf("x402 paid requests require HTTPS (got %s). The signed transaction must not travel over plaintext", u.Scheme)
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(strings.ToUpper(method), targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Botwallet-CLI/x402")
	req.Header.Set("X-Payment", xPayment)

	client := &http.Client{Timeout: requestTimeout}
	return client.Do(req)
}

// ReadResponseBody reads a response body with a size cap to prevent OOM.
func ReadResponseBody(resp *http.Response) (string, error) {
	limited := io.LimitReader(resp.Body, maxResponseBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	if len(data) > maxResponseBody {
		return string(data[:maxResponseBody]), fmt.Errorf("response body exceeds %d bytes (truncated)", maxResponseBody)
	}
	return string(data), nil
}

// ResponseHeaders extracts a simple map of response headers for display.
func ResponseHeaders(resp *http.Response) map[string]string {
	h := make(map[string]string)
	for k := range resp.Header {
		h[k] = resp.Header.Get(k)
	}
	return h
}
