package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// An API Botwallet would pay over plain HTTP is refused when it is probed:
// nothing is prepared, so the owner is never asked to approve a payment
// that could not be sent. The URL checked is the one that asked for
// payment, after redirects.
func TestX402ProbeOfAnHTTPAPIPreparesNothing(t *testing.T) {
	external := fakeExternalAPI(t)
	proxy := []string{"HTTP_PROXY=" + external.URL, "http_proxy=" + external.URL, "NO_PROXY=", "no_proxy="}
	for _, path := range []string{"/paid", "/moved"} {
		home := t.TempDir()
		addTestWallet(t, home, "main")
		api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
			return apiOK(map[string]interface{}{"fetch_id": "f-1", "status": "pre_approved"})
		})

		run := runCLI(t, home, api.URL, proxy, "x402", "fetch", "http://api.example.test"+path)
		out := run.json(t)
		if run.exitCode == 0 || out["error"] != "VALIDATION_ERROR" {
			t.Fatalf("%s: exit %d, %v; want VALIDATION_ERROR", path, run.exitCode, out)
		}
		if msg, _ := out["message"].(string); !strings.Contains(msg, "HTTPS") || !strings.Contains(msg, "Nothing was prepared or paid") {
			t.Errorf("%s: message = %q", path, msg)
		}
		if details, _ := out["details"].(map[string]interface{}); details["url"] != "http://api.example.test/paid" {
			t.Errorf("%s: details = %v, want the URL that asked for payment", path, out["details"])
		}
		if calls := api.called(); len(calls) != 0 {
			t.Errorf("%s: Botwallet API was called: %v", path, calls)
		}
	}
}

// A request for payment from another host than the one probed prepares
// nothing. The headers passed to x402 fetch confirm are meant for the
// probed host, and the probe kept Authorization from the other one, so the
// agent has to probe that URL itself. The message names that URL.
func TestX402ProbeAnsweredByAnotherHostPreparesNothing(t *testing.T) {
	external := fakeExternalAPI(t)
	proxy := []string{"HTTP_PROXY=" + external.URL, "http_proxy=" + external.URL, "NO_PROXY=", "no_proxy="}
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"fetch_id": "f-1", "status": "pre_approved"})
	})

	run := runCLI(t, home, api.URL, proxy, "x402", "fetch", "http://api.example.test/elsewhere",
		"--header", "Authorization: Bearer for-api-example-test")
	out := run.json(t)
	if run.exitCode == 0 || out["error"] != "VALIDATION_ERROR" {
		t.Fatalf("exit %d, %v; want VALIDATION_ERROR", run.exitCode, out)
	}
	msg, _ := out["message"].(string)
	for _, want := range []string{"api.example.test redirected to another host, pay.other.test (http://pay.other.test/paid)", "Nothing was prepared or paid"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message = %q, want it to contain %q", msg, want)
		}
	}
	if details, _ := out["details"].(map[string]interface{}); details["url"] != "http://pay.other.test/paid" {
		t.Errorf("details = %v, want the URL that asked for payment", out["details"])
	}
	if calls := api.called(); len(calls) != 0 {
		t.Errorf("Botwallet API was called: %v", calls)
	}
}

// A --header that is not "Name: value" stops x402 fetch and fetch confirm
// before anything is sent, instead of being dropped from the request.
func TestX402MalformedHeaderStopsEarly(t *testing.T) {
	// Stands in for the API (through HTTP_PROXY) and records any request.
	external := newFakeAPI(t, func(string, map[string]interface{}) (int, interface{}) {
		return http.StatusOK, "ok"
	})
	proxy := []string{"HTTP_PROXY=" + external.URL, "http_proxy=" + external.URL, "NO_PROXY=", "no_proxy="}

	for _, args := range [][]string{
		{"x402", "fetch", "http://api.example.test/free", "--header", "Accept application/json"},
		{"x402", "fetch", "http://api.example.test/free", "--header", ": no name"},
		{"x402", "fetch", "confirm", "fetch-1", "--header", "X-Api-Key"},
	} {
		home, server := newSigningServer(t)
		api := newFakeAPI(t, server.handle)
		run := runCLI(t, home, api.URL, proxy, args...)
		if code := run.json(t)["error"]; run.exitCode == 0 || code != "VALIDATION_ERROR" {
			t.Errorf("%v: exit %d, error = %v, want VALIDATION_ERROR", args, run.exitCode, code)
		}
		if calls := api.called(); len(calls) != 0 {
			t.Errorf("%v: Botwallet API was called: %v", args, calls)
		}
	}
	if calls := external.called(); len(calls) != 0 {
		t.Errorf("%d requests went out to the API", len(calls))
	}
}

// A paid request the CLI cannot send must be refused before Key 1 signs:
// x402_settle would otherwise record it as a failed call and Botwallet
// would deduct its price, although nothing left this machine.
func TestX402ConfirmRefusesUnsendableRequestsBeforeSigning(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		flags []string
		want  string
	}{
		{"plain http", "http://api.example.test/paid", nil, "HTTPS"},
		{"private address", "https://10.0.0.7/paid", nil, "private"},
		{"bad header name", "https://api.example.test/paid", []string{"--header", "Bad Header: v"}, "header name"},
		{"bad method", "https://api.example.test/paid", []string{"--method", "GE T"}, "method"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, server := newSigningServer(t)
			server.confirm["url"] = c.url
			api := newFakeAPI(t, server.handle)

			args := append([]string{"x402", "fetch", "confirm", "fetch-1"}, c.flags...)
			out := checkError(t, home, api, args, "VALIDATION_ERROR", "botwallet x402 fetch confirm fetch-1", "public HTTPS address")
			if msg, _ := out["message"].(string); !strings.Contains(msg, c.want) || !strings.Contains(msg, "Nothing was signed or paid") {
				t.Errorf("message = %q, want it to name %q and say nothing was paid", msg, c.want)
			}
			if details, _ := out["details"].(map[string]interface{}); details["fetch_id"] != "fetch-1" {
				t.Errorf("details = %v, want the fetch_id", out["details"])
			}
			for _, action := range []string{"frost_sign_init", "x402_sign_complete", "x402_settle"} {
				if api.received(action) {
					t.Errorf("%s was called for a request that cannot be sent: %v", action, api.called())
				}
			}
		})
	}
}
