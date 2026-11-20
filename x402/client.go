// =============================================================================
// x402 External HTTP Client
// =============================================================================
// Makes direct HTTP requests to external x402-protected APIs.
// The Botwallet server NEVER proxies API data — this client fetches directly.
//
// Security:
//   - Rejects private and local addresses (as IPs, numbers or names under
//     localhost), localhost, and non-HTTP(S) schemes
//   - Probes (Fetch) follow at most 5 redirects, check every new address
//     the same way, and drop credentials when the origin changes
//   - A request for payment from another host than the one probed is not
//     prepared (CheckAnsweringHost): the caller's headers were meant for
//     the probed host
//   - Paid requests (FetchWithPayment) enforce HTTPS and follow no
//     redirects — signed transactions go only to the checked URL and must
//     never travel over plaintext
//   - CheckPaidRequest refuses a paid request before Key 1 signs it
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
	"strconv"
	"strings"
	"time"
)

const (
	maxResponseBody = 10 * 1024 * 1024 // 10 MB cap
	requestTimeout  = 30 * time.Second
	maxRedirects    = 5 // followed by a probe, as by the MCP server

	// userAgent is sent with every request this package makes.
	userAgent = "Botwallet-CLI/x402"
)

// facilitatorClient sends the Bazaar discovery requests (DiscoverAPIs).
var facilitatorClient = &http.Client{Timeout: requestTimeout}

// probeClient sends probes (Fetch). See checkProbeRedirect.
var probeClient = &http.Client{Timeout: requestTimeout, CheckRedirect: checkProbeRedirect}

// paidClient sends paid requests (FetchWithPayment). It follows no
// redirects, so the signed payment goes to the checked URL and nowhere else:
// a redirect is the API's answer.
var paidClient = &http.Client{
	Timeout: requestTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// checkProbeRedirect is the redirect policy of probes: at most maxRedirects
// redirects, every new URL checked with ValidateURL, and no credentials
// (Authorization, Cookie, Proxy-Authorization) once a hop has left the
// first request's origin. net/http copies the first request's headers to
// every hop, so they are dropped from each later one.
func checkProbeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if err := ValidateURL(req.URL.String()); err != nil {
		return fmt.Errorf("redirected to a URL that cannot be probed: %w", err)
	}
	leftOrigin := !sameOrigin(req.URL, via[0].URL)
	for _, hop := range via[1:] {
		leftOrigin = leftOrigin || !sameOrigin(hop.URL, via[0].URL)
	}
	if leftOrigin {
		for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
			req.Header.Del(name)
		}
	}
	return nil
}

// sameOrigin reports whether a and b have the same scheme, host and port.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// hostName is a URL's host for comparing two URLs: without the port, without
// a trailing dot, and with A-Z in lower case. Only ASCII letters are folded:
// strings.ToLower also turns "İ" (U+0130) into "i" and the Kelvin sign
// (U+212A) into "k", which would make two different DNS names look equal.
func hostName(u *url.URL) string {
	host := []byte(strings.TrimSuffix(u.Hostname(), "."))
	for i, c := range host {
		if 'A' <= c && c <= 'Z' {
			host[i] = c + 'a' - 'A'
		}
	}
	return string(host)
}

// CheckAnsweringHost refuses a probe's answer that came from another host
// than the one probed (a subdomain is another host). x402 fetch prepares
// the payment for the URL that answered, and x402 fetch confirm sends the
// caller's headers there. Those headers were meant for the host that was
// probed, and the probe itself kept Authorization, Cookie and
// Proxy-Authorization from every other origin (checkProbeRedirect), so a
// request for payment from another host is not prepared. Another path, port
// or scheme on the same host is still the API that was probed.
//
// Hosts are compared as written. The same host written another way (an
// international name in Unicode and in punycode, two forms of one IP
// address) counts as another host: the agent probes the URL it is given.
func CheckAnsweringHost(probedURL string, answered *url.URL) error {
	probed, err := url.Parse(probedURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if hostName(probed) != hostName(answered) {
		return fmt.Errorf("%s redirected to another host, %s", hostName(probed), hostName(answered))
	}
	return nil
}

// blockedNetworks are the addresses that are not on the public internet:
// this machine, private networks, link-local (cloud metadata at
// 169.254.169.254), carrier-grade NAT, multicast and reserved ranges. The
// MCP server refuses the same ones (validateUrl in packages/mcp/src/x402).
var blockedNetworks = parseNetworks(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16",
	"224.0.0.0/4", "240.0.0.0/4",
	// ::, ::1 and the old IPv4-compatible form ::a.b.c.d
	"::/96", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
)

func parseNetworks(cidrs ...string) []*net.IPNet {
	networks := make([]*net.IPNet, len(cidrs))
	for i, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks[i] = n
	}
	return networks
}

// isBlockedIP reports whether ip is not on the public internet. An
// IPv4-mapped IPv6 address (::ffff:a.b.c.d) is checked as the IPv4 address.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range blockedNetworks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// parseNumericHost reads a host name whose last label is a number (decimal,
// or hex with 0x) as an IPv4 address, the way browsers and Node parse URLs:
// 2130706433, 0x7f.1 and 0177.0.0.1 are all 127.0.0.1. ok is false for any
// other host, and err is set for a number that is no IPv4 address.
func parseNumericHost(host string) (ip net.IP, ok bool, err error) {
	labels := strings.Split(host, ".")
	if !isIPv4Number(labels[len(labels)-1]) {
		return nil, false, nil
	}
	if len(labels) > 4 {
		return nil, true, fmt.Errorf("invalid IPv4 address %q", host)
	}
	numbers := make([]uint64, len(labels))
	for i, label := range labels {
		n, valid := parseIPv4Number(label)
		if !valid || (i < len(labels)-1 && n > 255) {
			return nil, true, fmt.Errorf("invalid IPv4 address %q", host)
		}
		numbers[i] = n
	}
	last := numbers[len(numbers)-1]
	if last >= 1<<(8*(5-len(numbers))) {
		return nil, true, fmt.Errorf("invalid IPv4 address %q", host)
	}
	value := last
	for i, n := range numbers[:len(numbers)-1] {
		value += n << (8 * (3 - i))
	}
	return net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value)), true, nil
}

// isIPv4Number reports whether a host's last label makes it an IPv4
// address: all decimal digits, or 0x and hex digits.
func isIPv4Number(label string) bool {
	if strings.HasPrefix(label, "0x") || strings.HasPrefix(label, "0X") {
		return strings.Trim(label[2:], "0123456789abcdefABCDEF") == ""
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}

// parseIPv4Number reads one part of an IPv4 address: decimal, octal (a
// leading 0) or hex (0x).
func parseIPv4Number(part string) (uint64, bool) {
	base := 10
	switch {
	case strings.HasPrefix(part, "0x") || strings.HasPrefix(part, "0X"):
		base, part = 16, part[2:]
	case len(part) > 1 && part[0] == '0':
		base, part = 8, part[1:]
	}
	if part == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(part, base, 64)
	return n, err == nil && n <= 0xffffffff
}

// ValidateURL rejects URLs that point to private/internal networks: a local
// host name (localhost or a name under it) or an address that is not on the
// public internet, also when written as a number (2130706433) or with an
// IPv6 zone.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("only http/https URLs are allowed, got %q", u.Scheme)
	}

	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))

	if hostname == "localhost" || hostname == "" || strings.HasSuffix(hostname, ".localhost") {
		return fmt.Errorf("localhost URLs are not allowed")
	}

	ip := net.ParseIP(hostname)
	if ip == nil && strings.Contains(hostname, "%") {
		// An IPv6 address with a zone (fe80::1%eth0) is on a local link
		return fmt.Errorf("private/internal IP addresses are not allowed: %s", hostname)
	}
	if ip == nil {
		numeric, isNumber, err := parseNumericHost(hostname)
		if err != nil {
			return fmt.Errorf("invalid URL: %w", err)
		}
		if isNumber {
			ip = numeric
		}
	}
	if ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("private/internal IP addresses are not allowed: %s", hostname)
	}

	return nil
}

// CheckPaidRequest returns the error FetchWithPayment would refuse this
// request with before sending anything: the URL (ValidateURL, HTTPS only),
// the method and the header names and values (the rules net/http applies).
// Call it before Key 1 signs, so a request that cannot be sent is never
// paid for.
func CheckPaidRequest(targetURL, method string, headers map[string]string) error {
	if err := ValidateURL(targetURL); err != nil {
		return err
	}
	u, _ := url.Parse(targetURL)
	if strings.ToLower(u.Scheme) != "https" {
		return fmt.Errorf("x402 paid requests require HTTPS (got %s). The signed transaction must not travel over plaintext", u.Scheme)
	}
	// "" means GET, as in http.NewRequest; the request is sent upper-cased
	if m := strings.ToUpper(method); m != "" && !isToken(m) {
		return fmt.Errorf("invalid HTTP method %q", method)
	}
	for name, value := range headers {
		if !isToken(name) {
			return fmt.Errorf("invalid HTTP header name %q", name)
		}
		if !validHeaderValue(value) {
			return fmt.Errorf("invalid value for HTTP header %q", name)
		}
	}
	return nil
}

// isToken reports whether s is a non-empty HTTP token (RFC 7230 tchar), as
// net/http requires of methods and header names.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// validHeaderValue reports whether net/http sends v as a header value: no
// control characters other than tab.
func validHeaderValue(v string) bool {
	for i := 0; i < len(v); i++ {
		if c := v[i]; (c < ' ' && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// newRequest builds a request to targetURL with the caller's headers and
// this package's User-Agent.
func newRequest(targetURL, method string, headers map[string]string, body string) (*http.Request, error) {
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
	req.Header.Set("User-Agent", userAgent)
	return req, nil
}

// Fetch makes a direct HTTP request to an external API without any payment
// header. It follows redirects (checkProbeRedirect): resp.Request is the
// request that got the answer, with the URL and method to pay.
func Fetch(targetURL, method string, headers map[string]string, body string) (*http.Response, error) {
	if err := ValidateURL(targetURL); err != nil {
		return nil, err
	}
	req, err := newRequest(targetURL, method, headers, body)
	if err != nil {
		return nil, err
	}
	return probeClient.Do(req)
}

// FetchWithPayment retries the request with the X-Payment header attached.
// Enforces HTTPS — a signed Solana transaction must never travel over
// plaintext — and returns a redirect as the answer instead of following it.
func FetchWithPayment(targetURL, method string, headers map[string]string, body string, xPayment string) (*http.Response, error) {
	if err := CheckPaidRequest(targetURL, method, headers); err != nil {
		return nil, err
	}
	req, err := newRequest(targetURL, method, headers, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Payment", xPayment)
	return paidClient.Do(req)
}

// readCapped reads r up to max bytes. truncated reports that r held more,
// in which case data is its first max bytes.
func readCapped(r io.Reader, max int64) (data []byte, truncated bool, err error) {
	data, err = io.ReadAll(io.LimitReader(r, max+1))
	if int64(len(data)) > max {
		return data[:max], true, err
	}
	return data, false, err
}

// ReadResponseBody reads a response body with a size cap to prevent OOM.
func ReadResponseBody(resp *http.Response) (string, error) {
	data, truncated, err := readCapped(resp.Body, maxResponseBody)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	if truncated {
		return string(data), fmt.Errorf("response body exceeds %d bytes (truncated)", maxResponseBody)
	}
	return string(data), nil
}
