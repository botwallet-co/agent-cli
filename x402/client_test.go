package x402

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The addresses the MCP server refuses (packages/mcp/tests/unit/x402),
// however they are written, plus an IPv6 zone.
func TestValidateURLRefusesLocalAddresses(t *testing.T) {
	for _, u := range []string{
		"http://localhost:8080/secret",
		"http://LOCALHOST./",
		"http://api.localhost/",
		"http://127.0.0.1/",
		"http://127.0.0.2/",
		"http://2130706433/",
		"http://0x7f.1/",
		"http://0177.0.0.1/",
		"http://0.0.0.0/",
		"http://[::1]:8080/",
		"http://[::]/",
		"http://[::ffff:127.0.0.1]/",
		"http://[::ffff:a9fe:a9fe]/",
		"http://[::7f00:1]/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]/",
		"http://[fe80::1]/",
		"http://[fe80::1%25eth0]/",
		"http://10.1.2.3/",
		"http://172.16.0.1/",
		"http://172.31.255.255/",
		"http://192.168.1.1/",
		"http://100.64.0.1/",
		"http://100.100.100.200/",
		"http://224.0.0.1/",
		"http://240.0.0.1/",
		"http://[fec0::1]/",
		"http://[ff02::1]/",
	} {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%s) accepted it", u)
		}
	}

	for _, u := range []string{
		"https://api.example.com/data",
		"http://8.8.8.8/",
		"https://[2606:4700:4700::1111]/",
		"http://172.32.0.1/",
		"http://100.128.0.1/",
		"http://134744072/",
		"https://api.example.com.:8443/data",
	} {
		if err := ValidateURL(u); err != nil {
			t.Errorf("ValidateURL(%s) = %v, want it accepted", u, err)
		}
	}

	// A host name ending in a number is an IPv4 address, or not a valid host
	for _, u := range []string{"http://example.123/", "http://1.2.3.4.5/", "http://256.1.1.1/", "http://4294967296/"} {
		if err := ValidateURL(u); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("ValidateURL(%s) = %v, want an invalid URL", u, err)
		}
	}
}

// CheckPaidRequest must refuse exactly what FetchWithPayment cannot send,
// so it can run before Key 1 signs.
func TestCheckPaidRequest(t *testing.T) {
	ok := []struct {
		name, url, method string
		headers           map[string]string
	}{
		{"plain GET", "https://api.example.com/data", "GET", nil},
		{"no method means GET", "https://api.example.com/data", "", nil},
		{"lower-case method", "https://api.example.com/data", "post", nil},
		{"headers", "https://api.example.com/data", "GET", map[string]string{"Accept": "application/json", "X-Api-Key": "k\tv"}},
	}
	for _, c := range ok {
		if err := CheckPaidRequest(c.url, c.method, c.headers); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}

	refused := []struct {
		name, url, method string
		headers           map[string]string
		want              string
	}{
		{"plain HTTP", "http://api.example.com/data", "GET", nil, "require HTTPS"},
		{"private address", "https://10.0.0.1/data", "GET", nil, "private"},
		{"localhost", "https://localhost/data", "GET", nil, "localhost"},
		{"other scheme", "ftp://api.example.com/data", "GET", nil, "only http/https"},
		{"method with a space", "https://api.example.com/data", "GE T", nil, "invalid HTTP method"},
		{"header name with a space", "https://api.example.com/data", "GET", map[string]string{"Bad Header": "v"}, "invalid HTTP header name"},
		{"empty header name", "https://api.example.com/data", "GET", map[string]string{"": "v"}, "invalid HTTP header name"},
		{"header value with a newline", "https://api.example.com/data", "GET", map[string]string{"X-A": "a\nb"}, "invalid value"},
	}
	for _, c := range refused {
		err := CheckPaidRequest(c.url, c.method, c.headers)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want one containing %q", c.name, err, c.want)
		}
	}
}

// Whatever CheckPaidRequest lets through, net/http sends.
func TestCheckedRequestsAreSent(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Method + " " + r.Header.Get("X-Weird") + " " + r.Header.Get("X-Payment")))
	}))
	defer srv.Close()

	headers := map[string]string{"X-Weird": "!#$%&'*+-.^_`|~ value\twith tab"}
	if err := CheckPaidRequest("https://api.example.com/", "patch", headers); err != nil {
		t.Fatal(err)
	}
	// The test server is on 127.0.0.1, which ValidateURL refuses, so the
	// request is made directly with this package's request builder.
	req, err := newRequest(srv.URL, "patch", headers, "")
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Payment", "signed")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "PATCH "+headers["X-Weird"]+" signed" {
		t.Errorf("server saw %q", body)
	}

	// And what it refuses, net/http would not send either, so refusing
	// early never stops a request that used to work.
	for _, bad := range []struct {
		method  string
		headers map[string]string
	}{
		{"GE T", nil},
		{"GET", map[string]string{"Bad Header": "v"}},
		{"GET", map[string]string{"": "v"}},
		{"GET", map[string]string{"X-A": "a\nb"}},
		{"GET", map[string]string{"X-A": "a\x7fb"}},
	} {
		if err := CheckPaidRequest("https://api.example.com/", bad.method, bad.headers); err == nil {
			t.Errorf("CheckPaidRequest(%q, %v) accepted it", bad.method, bad.headers)
		}
		req, err := newRequest(srv.URL, bad.method, bad.headers, "")
		if err == nil {
			var resp *http.Response
			if resp, err = srv.Client().Do(req); err == nil {
				resp.Body.Close()
			}
		}
		if err == nil {
			t.Errorf("net/http sent method %q with headers %v", bad.method, bad.headers)
		}
	}
}

// fakeTransport stands in for the network: it records every request and
// answers it with answer, as http.Transport would (Request set, a Body).
type fakeTransport struct {
	mu     sync.Mutex
	seen   []*http.Request
	answer func(*http.Request) *http.Response
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.seen = append(f.seen, req)
	f.mu.Unlock()
	resp := f.answer(req)
	resp.Request = req
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	resp.Body = io.NopCloser(strings.NewReader(""))
	return resp, nil
}

func (f *fakeTransport) requests() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.seen...)
}

// useFakeTransport makes client send through a fakeTransport until the test ends.
func useFakeTransport(t *testing.T, client *http.Client, answer func(*http.Request) *http.Response) *fakeTransport {
	t.Helper()
	f := &fakeTransport{answer: answer}
	old := client.Transport
	client.Transport = f
	t.Cleanup(func() { client.Transport = old })
	return f
}

func redirectTo(status int, location string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Location": {location}}}
}

// The signed payment goes to the checked URL only. A redirect is the API's
// answer and is never followed, to plain HTTP or anywhere else.
func TestFetchWithPaymentFollowsNoRedirect(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		rt := useFakeTransport(t, paidClient, func(*http.Request) *http.Response {
			return redirectTo(status, "http://other-host.example/steal")
		})
		resp, err := FetchWithPayment("https://api.example.com/paid", "GET", nil, "", "SIGNED-TX")
		if err != nil {
			t.Fatalf("%d: %v", status, err)
		}
		resp.Body.Close()
		if resp.StatusCode != status || resp.Header.Get("Location") != "http://other-host.example/steal" {
			t.Errorf("%d: answer = %d %q, want the redirect itself", status, resp.StatusCode, resp.Header.Get("Location"))
		}
		seen := rt.requests()
		if len(seen) != 1 || seen[0].URL.String() != "https://api.example.com/paid" || seen[0].Header.Get("X-Payment") != "SIGNED-TX" {
			t.Errorf("%d: requests sent = %v, want only the paid URL, with the payment", status, seen)
		}
	}
}

// A probe checks every address it is sent to, as the first one.
func TestFetchChecksEveryRedirect(t *testing.T) {
	for _, location := range []string{"http://169.254.169.254/latest/meta-data/", "http://localhost:8080/", "ftp://files.example.com/"} {
		rt := useFakeTransport(t, probeClient, func(*http.Request) *http.Response {
			return redirectTo(http.StatusFound, location)
		})
		if _, err := Fetch("https://api.example.com/data", "GET", nil, ""); err == nil || !strings.Contains(err.Error(), "cannot be probed") {
			t.Errorf("redirect to %s: error = %v, want a refusal", location, err)
		}
		if n := len(rt.requests()); n != 1 {
			t.Errorf("redirect to %s: %d requests sent, want 1", location, n)
		}
	}
}

func TestFetchStopsAfterFiveRedirects(t *testing.T) {
	rt := useFakeTransport(t, probeClient, func(req *http.Request) *http.Response {
		n, _ := strconv.Atoi(strings.TrimPrefix(req.URL.Path, "/hop"))
		return redirectTo(http.StatusFound, fmt.Sprintf("/hop%d", n+1))
	})
	if _, err := Fetch("https://api.example.com/hop0", "GET", nil, ""); err == nil || !strings.Contains(err.Error(), "stopped after 5 redirects") {
		t.Errorf("error = %v, want it to stop after 5 redirects", err)
	}
	if n := len(rt.requests()); n != 6 {
		t.Errorf("%d requests sent, want 6 (the probe and 5 redirects)", n)
	}

	// Five are followed.
	useFakeTransport(t, probeClient, func(req *http.Request) *http.Response {
		if req.URL.Path == "/hop5" {
			return &http.Response{StatusCode: http.StatusPaymentRequired}
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(req.URL.Path, "/hop"))
		return redirectTo(http.StatusFound, fmt.Sprintf("/hop%d", n+1))
	})
	resp, err := Fetch("https://api.example.com/hop0", "GET", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired || resp.Request.URL.Path != "/hop5" {
		t.Errorf("answer = %d from %s, want 402 from /hop5", resp.StatusCode, resp.Request.URL)
	}
}

// Credentials go only to the origin they were given for: not after a
// redirect to another host (a subdomain too) or from HTTPS to HTTP, and not
// after coming back. Other headers stay.
func TestFetchDropsCredentialsOutsideTheOrigin(t *testing.T) {
	rt := useFakeTransport(t, probeClient, func(req *http.Request) *http.Response {
		switch req.URL.String() {
		case "https://api.example.com/start":
			return redirectTo(http.StatusFound, "/same-origin")
		case "https://api.example.com/same-origin":
			return redirectTo(http.StatusFound, "https://eu.api.example.com/subdomain")
		case "https://eu.api.example.com/subdomain":
			return redirectTo(http.StatusFound, "https://api.example.com/back")
		}
		return &http.Response{StatusCode: http.StatusPaymentRequired}
	})
	headers := map[string]string{"Authorization": "Bearer secret", "Cookie": "session=1", "X-Api-Key": "key"}
	resp, err := Fetch("https://api.example.com/start", "GET", headers, "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	seen := rt.requests()
	if len(seen) != 4 {
		t.Fatalf("%d requests sent, want 4", len(seen))
	}
	for i, req := range seen {
		withCredentials := i < 2 // the first and the same-origin hop
		if got := req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != ""; got != withCredentials {
			t.Errorf("request %d to %s: credentials sent = %v, want %v", i, req.URL, got, withCredentials)
		}
		if req.Header.Get("X-Api-Key") != "key" {
			t.Errorf("request %d to %s: X-Api-Key dropped", i, req.URL)
		}
	}

	// From HTTPS to HTTP on the same host is another origin too.
	rt = useFakeTransport(t, probeClient, func(req *http.Request) *http.Response {
		if req.URL.Scheme == "https" {
			return redirectTo(http.StatusFound, "http://api.example.com/plain")
		}
		return &http.Response{StatusCode: http.StatusPaymentRequired}
	})
	resp, err = Fetch("https://api.example.com/start", "GET", headers, "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if seen := rt.requests(); len(seen) != 2 || seen[1].Header.Get("Authorization") != "" {
		t.Errorf("after HTTPS to HTTP: requests %v, want the second without Authorization", seen)
	}
}

// The probe reports the request that got the answer: after a 303, a GET to
// the new URL.
func TestFetchReportsTheRequestThatGotTheAnswer(t *testing.T) {
	useFakeTransport(t, probeClient, func(req *http.Request) *http.Response {
		if req.URL.Path == "/submit" {
			return redirectTo(http.StatusSeeOther, "/result")
		}
		return &http.Response{StatusCode: http.StatusPaymentRequired}
	})
	resp, err := Fetch("https://api.example.com/submit", "post", nil, `{"q":1}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.Method != "GET" || resp.Request.URL.String() != "https://api.example.com/result" {
		t.Errorf("answered by %s %s, want GET https://api.example.com/result", resp.Request.Method, resp.Request.URL)
	}
}

// A request for payment is prepared only for the host that was probed.
// Another path, port or scheme on that host is the same API; any other
// host is not, a subdomain or a parent domain included.
func TestCheckAnsweringHost(t *testing.T) {
	parse := func(raw string) *url.URL {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}

	const probed = "https://api.example.com/data"
	for _, answered := range []string{
		"https://api.example.com/data",
		"https://api.example.com/v2/data?q=1",
		"https://api.example.com:8443/data",
		"https://API.Example.COM/data",
		"https://api.example.com./data",
	} {
		if err := CheckAnsweringHost(probed, parse(answered)); err != nil {
			t.Errorf("answer from %s: %v, want it accepted", answered, err)
		}
	}
	// A redirect from HTTP to HTTPS on the same host is the same API too.
	if err := CheckAnsweringHost("http://api.example.com/data", parse(probed)); err != nil {
		t.Errorf("HTTP to HTTPS on the same host: %v, want it accepted", err)
	}

	for _, answered := range []string{
		"https://pay.other.example/data",
		"https://eu.api.example.com/data",
		"https://example.com/data",
		"https://api.example.com.evil.example/data",
		"https://93.184.215.14/data",
	} {
		u := parse(answered)
		err := CheckAnsweringHost(probed, u)
		if err == nil || !strings.Contains(err.Error(), "api.example.com redirected to another host, "+u.Hostname()) {
			t.Errorf("answer from %s: error = %v, want a refusal naming both hosts", answered, err)
		}
	}

	// Only A-Z are folded. "İ" (U+0130) and the Kelvin sign (U+212A) become
	// "i" and "k" in strings.ToLower, but they are other DNS names.
	for _, pair := range [][2]string{
		{"https://api.stripe.example/data", "https://api.str\u0130pe.example/pay"},
		{"https://api.kiosk.example/data", "https://api.\u212Aiosk.example/pay"},
	} {
		if err := CheckAnsweringHost(pair[0], parse(pair[1])); err == nil {
			t.Errorf("probe of %s answered by %s: accepted, want a refusal", pair[0], pair[1])
		}
	}

	// What a probe reports after redirects is what gets checked.
	useFakeTransport(t, probeClient, func(req *http.Request) *http.Response {
		switch req.URL.String() {
		case "https://api.example.com/moved":
			return redirectTo(http.StatusFound, "/data")
		case "https://api.example.com/elsewhere":
			return redirectTo(http.StatusFound, "https://pay.other.example/data")
		}
		return &http.Response{StatusCode: http.StatusPaymentRequired}
	})
	for target, wantRefused := range map[string]bool{
		"https://api.example.com/moved":     false,
		"https://api.example.com/elsewhere": true,
	} {
		resp, err := Fetch(target, "GET", map[string]string{"Authorization": "Bearer secret"}, "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if err := CheckAnsweringHost(target, resp.Request.URL); (err != nil) != wantRefused {
			t.Errorf("probe of %s, answered by %s: error = %v, want refused = %v", target, resp.Request.URL, err, wantRefused)
		}
	}
}

func TestReadResponseBodyCap(t *testing.T) {
	resp := func(body string) *http.Response {
		return &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	}
	if got, err := ReadResponseBody(resp("hello")); err != nil || got != "hello" {
		t.Errorf("ReadResponseBody = %q, %v", got, err)
	}

	exact := strings.Repeat("x", maxResponseBody)
	if got, err := ReadResponseBody(resp(exact)); err != nil || len(got) != maxResponseBody {
		t.Errorf("body at the cap: len %d, %v", len(got), err)
	}
	got, err := ReadResponseBody(resp(exact + "y"))
	if err == nil || !strings.Contains(err.Error(), "truncated") || len(got) != maxResponseBody {
		t.Errorf("body over the cap: len %d, %v; want the first %d bytes and an error", len(got), err, maxResponseBody)
	}
}

func TestMatchKeyword(t *testing.T) {
	items := []DiscoveredResource{
		{Resource: "https://weather.example/forecast"},
		{Resource: "https://a.example/x", Metadata: map[string]interface{}{"description": "Stock Quotes"}},
		{Resource: "https://b.example/y", Metadata: map[string]interface{}{
			"output": map[string]interface{}{"example": map[string]interface{}{"temperature": 20}},
		}},
		{Resource: "https://c.example/z", Metadata: map[string]interface{}{"description": 42}},
	}
	for q, want := range map[string]string{
		"WEATHER": "https://weather.example/forecast",
		"quotes":  "https://a.example/x",
		"temper":  "https://b.example/y",
	} {
		got := MatchKeyword(items, q)
		if len(got) != 1 || got[0].Resource != want {
			t.Errorf("MatchKeyword(%q) = %+v, want only %s", q, got, want)
		}
	}
	if got := MatchKeyword(items, "nothing-matches"); len(got) != 0 {
		t.Errorf("MatchKeyword found %+v", got)
	}
	if got := MatchKeyword(items, ""); len(got) != len(items) {
		t.Errorf("empty query kept %d of %d", len(got), len(items))
	}
}

func TestRawAmountToUSDC(t *testing.T) {
	for raw, want := range map[string]float64{"1000000": 1, "2500": 0.0025, "0": 0, "abc": 0, "": 0} {
		if got := RawAmountToUSDC(raw); got != want {
			t.Errorf("RawAmountToUSDC(%q) = %v, want %v", raw, got, want)
		}
	}
}
