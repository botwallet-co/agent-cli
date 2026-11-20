package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/x402"
)

var x402Cmd = &cobra.Command{
	Use:   "x402",
	Short: "Interact with x402 paid APIs",
	Long: `Access paid APIs using the x402 payment protocol.

Two-step flow (same pattern as pay/pay confirm):

STEP 1: botwallet x402 fetch <url>
        Probes the API. If free, returns data. If paid, shows price
        and returns a fetch_id.

STEP 2: botwallet x402 fetch confirm <fetch_id>
        FROST signs a payment, sends it with the request, and returns
        the API response data.

Agents can probe multiple APIs to compare prices ("window shopping")
before committing to any payment.

Subcommands:
  fetch     Probe a URL or pay+fetch data
  discover  List verified paid APIs, or search the x402 Bazaar with --bazaar`,
	Example: `  # Window shopping: probe multiple APIs
  botwallet x402 fetch https://api.weather.com/forecast
  botwallet x402 fetch https://api.other-weather.com/data

  # Pay the cheapest one
  botwallet x402 fetch confirm <fetch_id>

  # Discover APIs
  botwallet x402 discover "weather forecast"`,
}

var (
	x402FetchMethod  string
	x402FetchBody    string
	x402FetchHeaders []string
)

var x402FetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "Probe a paid API (Step 1 of 2)",
	Long: `Probe an x402 API to see its price without paying.

This command:
1. Makes a direct HTTP request to the URL (no payment), following up to 5
   redirects to public addresses
2. If the API is free (200), returns the data immediately
3. If the API requires payment (402), parses the price and:
   - Stops unless the URL that asked for payment is a public HTTPS address,
     the only kind Botwallet pays, on the host you probed. After a redirect
     to another host, probe that URL directly
   - Checks your guard rails (auto-approve, limits, budget)
   - Creates a payment intent on the server for that URL
   - Returns fetch_id, price, and status

No money is spent. The agent can probe many APIs to compare prices.

Guard rails apply:
  GREEN  → pre_approved: ready to confirm
  YELLOW → awaiting_approval: needs human owner approval
  RED    → rejected: blocked by guard rails`,
	Example: `  # Probe a weather API
  botwallet x402 fetch https://api.weather.com/forecast

  # POST with body
  botwallet x402 fetch https://api.data.com/query --method POST --body '{"q":"test"}'

  # With custom headers
  botwallet x402 fetch https://api.example.com/data --header "Accept: application/json"`,
	Args: cobra.ExactArgs(1),
	Run:  runX402Fetch,
}

func runX402Fetch(cmd *cobra.Command, args []string) {
	targetURL := args[0]

	// Validate URL locally before any network calls
	if err := x402.ValidateURL(targetURL); err != nil {
		exitValidationError(fmt.Sprintf("Invalid URL: %s", err), "Provide a valid public HTTP(S) URL")
		return
	}
	headers, err := parseHeaders(x402FetchHeaders)
	if err != nil {
		exitValidationError(err.Error(), headerHowToFix)
		return
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Probing %s ...", targetURL)
	}

	resp, err := x402.Fetch(targetURL, x402FetchMethod, headers, x402FetchBody)
	if err != nil {
		exitError("FETCH_FAILED", fmt.Sprintf("Failed to reach %s: %s", targetURL, err), "Check the URL and try again", nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		body, readErr := x402.ReadResponseBody(resp)
		if readErr != nil && body == "" {
			exitError("READ_FAILED", readErr.Error(), "The API response could not be read", nil)
			return
		}

		result := map[string]interface{}{
			"payment_required": false,
			"url":              targetURL,
			"status_code":      resp.StatusCode,
			"content_type":     resp.Header.Get("Content-Type"),
		}

		// Try to parse as JSON for cleaner output
		if isJSON(resp.Header.Get("Content-Type")) {
			result["response"] = parseJSONBody(body)
		} else {
			result["response_text"] = body
		}

		if resp.StatusCode >= 400 {
			result["message"] = fmt.Sprintf("API returned HTTP %d — no payment was required but the request failed. Try a different HTTP method (--method) or check the URL.", resp.StatusCode)
		}

		output.FormatX402Free(result, body)
		return
	}

	// 402 Payment Required — parse x402 response
	pr, err := x402.Parse402Response(resp)
	if err != nil {
		exitError("X402_PARSE_ERROR", fmt.Sprintf("Could not parse 402 response: %s", err),
			"The API may not implement the x402 standard correctly", nil)
		return
	}

	// Find a payment option in USDC on Solana
	solOpt := x402.FindSolanaOption(pr)
	if solOpt == nil {
		networks := x402.AvailableNetworks(pr)
		result := map[string]interface{}{
			"payment_required":   true,
			"url":                targetURL,
			"compatible":         false,
			"reason":             "No option to pay in USDC on Solana. Botwallet pays in USDC on Solana only.",
			"available_networks": networks,
			"options":            x402.AllSummaries(pr),
		}
		output.FormatX402Incompatible(result)
		return
	}

	// After any redirects, the request that got the 402 is the one to pay:
	// the paid request follows no redirects.
	paidURL, paidMethod := resp.Request.URL.String(), resp.Request.Method

	// The agent chose the host it probed, and the headers it passes to
	// x402 fetch confirm are meant for that host. A request for payment
	// from another host is not prepared: the agent probes that URL itself.
	if err := x402.CheckAnsweringHost(targetURL, resp.Request.URL); err != nil {
		exitError("VALIDATION_ERROR",
			fmt.Sprintf("Botwallet cannot pay this API: %s (%s). Nothing was prepared or paid", err, paidURL),
			"To pay that host, probe its URL directly and pass only the headers meant for it",
			map[string]interface{}{"url": paidURL})
		return
	}

	// Refuse before anything is prepared or sent to the owner for approval:
	// x402 fetch confirm would never send this request.
	if err := x402.CheckPaidRequest(paidURL, paidMethod, headers); err != nil {
		exitError("VALIDATION_ERROR",
			fmt.Sprintf("Botwallet cannot pay this API: %s. Nothing was prepared or paid", err),
			"Botwallet pays only APIs at a public HTTPS address. Probe the API's https:// URL if it has one",
			map[string]interface{}{"url": paidURL})
		return
	}

	client := authedClient()

	serverResult, err := client.X402Prepare(api.X402PrepareRequest{
		URL:     paidURL,
		PayTo:   solOpt.PayTo,
		Amount:  solOpt.GetAmount(),
		Network: x402.NormalizeSolanaNetwork(solOpt.Network),
		Method:  paidMethod,
		Asset:   solOpt.Asset,
		Scheme:  solOpt.Scheme,
	})
	if err != nil {
		handleAPIError(err)
		return
	}

	output.FormatX402Fetch(serverResult)
}

var (
	x402ConfirmMethod  string
	x402ConfirmBody    string
	x402ConfirmHeaders []string
)

var x402FetchConfirmCmd = &cobra.Command{
	Use:   "confirm <fetch_id>",
	Short: "Pay and retrieve data (Step 2 of 2)",
	Long: `Execute an x402 payment and retrieve the API data.

This command:
1. Retrieves the payment intent from the server
2. Checks that the paid request can be sent: an HTTPS URL at a public
   address and a valid method and headers. If not, it stops before
   anything is signed or paid
3. Builds a USDC transfer transaction
4. FROST threshold signs it (agent + server cooperate)
5. Sends the signed payment to the external API, and only there: a
   redirect is the API's answer and is not followed
6. Returns the API response data

The paid request goes to the URL, with the method, that the probe
recorded (x402 fetch records a URL only on the host it probed). Its body
and headers are not kept: if the probe used --body or --header, pass
them again here so the paid request matches what the API expects.
--method overrides the recorded method.

Only works for intents with status:
- PRE-APPROVED (guard rails passed)
- APPROVED (human owner approved)

Payment intents expire after 48 hours.`,
	Example: `  # Simple GET:
  botwallet x402 fetch confirm <fetch_id>

  # After a POST probe, pass its body (and any --header) again:
  botwallet x402 fetch confirm <fetch_id> --body '{"q":"test"}'`,
	Args: cobra.ExactArgs(1),
	Run:  runX402FetchConfirm,
}

func runX402FetchConfirm(cmd *cobra.Command, args []string) {
	fetchID := args[0]

	paidHeaders, err := parseHeaders(x402ConfirmHeaders)
	if err != nil {
		exitValidationError(err.Error()+". Nothing was signed or paid", headerHowToFix)
		return
	}

	wallet, client := authedWallet()

	target := x402Target(fetchID)

	// Load Key 1 first: if it is missing or belongs to another wallet,
	// stop before the server starts processing the payment.
	signer, err := loadSigningWallet(wallet)
	if err != nil {
		exitSigningError(err, target)
		return
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Confirming x402 payment %s...", fetchID)
	}

	// Step 2a: Server builds Solana transaction
	confirmResult, err := client.X402Confirm(fetchID)
	if err != nil {
		exitConfirmError(err, target)
		return
	}

	messageB64, ok := confirmResult["message"].(string)
	if !ok || messageB64 == "" {
		exitError("SIGNING_ERROR", "Server did not return a message to sign",
			"The payment intent may have expired. Run 'x402 fetch' again", nil)
		return
	}
	transactionID, ok := confirmResult["transaction_id"].(string)
	if !ok || transactionID == "" {
		exitError("SIGNING_ERROR", "Server did not return a transaction_id",
			"The payment intent may have expired. Run 'x402 fetch' again", nil)
		return
	}

	// The paid request is checked before Key 1 signs: one that cannot be
	// sent (no URL, plain HTTP, a private address, a bad method or header)
	// must never be paid for. The fetch stays pending on the server, so
	// confirm can run again once a bad --method or --header is fixed.
	targetURL, _ := confirmResult["url"].(string)
	if targetURL == "" {
		exitError("X402_ERROR", "Server did not return the target URL",
			"The payment intent may be corrupted. Run 'x402 fetch' again", nil)
		return
	}

	// Method: CLI flag > server-stored > default GET
	method, _ := confirmResult["method"].(string)
	if x402ConfirmMethod != "" {
		method = x402ConfirmMethod
	}
	if method == "" {
		method = "GET"
	}

	if err := x402.CheckPaidRequest(targetURL, method, paidHeaders); err != nil {
		exitError("VALIDATION_ERROR",
			fmt.Sprintf("Refusing to pay for this request: %s. Nothing was signed or paid", err),
			fmt.Sprintf("If a --method or --header you passed is the problem, fix it and run '%s' again. "+
				"Botwallet pays only APIs at a public HTTPS address", target.retry),
			map[string]interface{}{"fetch_id": fetchID})
		return
	}

	output.X402ConfirmSummary(confirmResult)

	intent, err := x402Intent(confirmResult)
	if err != nil {
		exitSigningError(err, target)
		return
	}

	// Step 2b: FROST threshold signing (returns signed tx, does NOT submit to Solana)
	signResult, err := frostSignForX402(client, transactionID, messageB64, signer, intent)
	if err != nil {
		exitSigningError(err, target)
		return
	}

	signedTxB64, ok := signResult["signed_transaction"].(string)
	if !ok || signedTxB64 == "" {
		exitError("SIGNING_ERROR", "Server did not return a signed transaction",
			target.retryAdvice(), nil)
		return
	}

	network, _ := confirmResult["network"].(string)

	xPayment, err := x402.BuildXPaymentHeader(signedTxB64, network)
	if err != nil {
		exitError("X402_ERROR", fmt.Sprintf("Failed to build payment header: %v", err),
			"This is unexpected. Try again", nil)
		return
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Fetching data with payment...")
	}

	// Step 2c: Retry the API with payment (body + headers forwarded from CLI flags)
	apiResp, err := x402.FetchWithPayment(targetURL, method, paidHeaders, x402ConfirmBody, xPayment)
	if err != nil {
		details := map[string]interface{}{"fetch_id": fetchID}
		if _, settleErr := client.X402Settle(fetchID, false, 0, err.Error()); settleErr != nil {
			details["settle_error"] = settleErr.Error()
			if output.IsHumanOutput() {
				output.WarningMsg("Additionally failed to report settlement: %v", settleErr)
			}
		}
		exitError("X402_FETCH_FAILED", fmt.Sprintf("Failed to reach API with payment: %s", err),
			"The payment transaction may still settle on-chain. Check your balance.", details)
		return
	}
	defer apiResp.Body.Close()

	body, readErr := x402.ReadResponseBody(apiResp)
	if readErr != nil && body == "" {
		body = fmt.Sprintf("(failed to read response: %v)", readErr)
	} else if readErr != nil {
		output.WarningMsg("Only part of the API's answer is shown: %v", readErr)
	}

	// Step 2d: Report outcome to Botwallet server
	apiSuccess := apiResp.StatusCode >= 200 && apiResp.StatusCode < 300
	settleResult, settleErr := client.X402Settle(fetchID, apiSuccess, apiResp.StatusCode, "")
	if settleErr != nil {
		if output.IsHumanOutput() {
			output.WarningMsg("Failed to report settlement: %v", settleErr)
		}
	}

	if !apiSuccess {
		errMsg := fmt.Sprintf("API returned %d after payment", apiResp.StatusCode)
		result := map[string]interface{}{
			"success":         false,
			"fetch_id":        fetchID,
			"url":             targetURL,
			"response_status": apiResp.StatusCode,
			"error":           errMsg,
		}
		if body != "" {
			result["response_body"] = body
		}
		var location string
		if apiResp.StatusCode >= 300 && apiResp.StatusCode < 400 {
			location = apiResp.Header.Get("Location")
		}
		output.FormatX402PaidError(result, body, location)
		return
	}

	result := map[string]interface{}{
		"success":         true,
		"fetch_id":        fetchID,
		"url":             targetURL,
		"response_status": apiResp.StatusCode,
		"content_type":    apiResp.Header.Get("Content-Type"),
	}

	if isJSON(apiResp.Header.Get("Content-Type")) {
		result["response"] = parseJSONBody(body)
	} else {
		result["response_text"] = body
	}

	if settleErr == nil && settleResult != nil {
		if v, ok := settleResult["amount_usdc"]; ok {
			result["amount_paid"] = v
		}
		if v, ok := settleResult["new_balance_usdc"]; ok {
			result["new_balance"] = v
		}
		if v, ok := settleResult["transaction_id"]; ok {
			result["transaction_id"] = v
		}
	} else if settleErr != nil {
		result["settle_error"] = settleErr.Error()
	}

	output.FormatX402FetchConfirm(result)
}

var (
	x402DiscoverLimit       int
	x402DiscoverOffset      int
	x402DiscoverAll         bool
	x402DiscoverBazaar      bool
	x402DiscoverFacilitator string
)

var x402DiscoverCmd = &cobra.Command{
	Use:   "discover [query]",
	Short: "Discover x402 paid APIs",
	Long: `Discover paid APIs available via the x402 protocol.

By default, shows Botwallet's curated catalog of verified Solana-compatible
APIs. These have been tested and confirmed working.

Use --bazaar to search the full x402 Bazaar (Coinbase CDP facilitator)
which has thousands of APIs across all networks.

If a query is provided, results are filtered by keyword match on
the API name, description, and category.`,
	Example: `  # List all verified Solana APIs (curated catalog)
  botwallet x402 discover

  # Search the catalog
  botwallet x402 discover "url content"

  # Search the full x402 Bazaar (all networks)
  botwallet x402 discover --bazaar

  # Bazaar: Solana-only
  botwallet x402 discover --bazaar "weather"

  # Bazaar: all networks
  botwallet x402 discover --bazaar --all`,
	Args: cobra.MaximumNArgs(1),
	Run:  runX402Discover,
}

func runX402Discover(cmd *cobra.Command, args []string) {
	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}

	if x402DiscoverBazaar {
		runX402DiscoverBazaar(query)
		return
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Loading verified x402 API catalog...")
	}

	entries, err := anonClient().X402Catalog(query)
	if err != nil {
		exitError("CATALOG_FAILED",
			fmt.Sprintf("Failed to load catalog: %s", err),
			"Check your network connection or use --bazaar to query the Coinbase facilitator directly",
			nil,
		)
		return
	}

	output.FormatX402Catalog(entries, query)
}

func runX402DiscoverBazaar(query string) {
	facilitatorURL := x402DiscoverFacilitator
	if facilitatorURL == "" {
		facilitatorURL = os.Getenv("X402_FACILITATOR_URL")
	}
	if facilitatorURL == "" {
		facilitatorURL = x402.DefaultFacilitatorURL
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Querying x402 Bazaar (Coinbase CDP facilitator)...")
	}

	dr, err := x402.DiscoverAPIs(facilitatorURL, x402DiscoverLimit, x402DiscoverOffset)
	if err != nil {
		exitError("DISCOVERY_FAILED",
			fmt.Sprintf("Failed to query facilitator: %s", err),
			"Check your network connection or try a different facilitator with --facilitator <url>",
			map[string]interface{}{
				"facilitator_url": facilitatorURL,
			},
		)
		return
	}

	items := dr.Items

	solanaOnly := !x402DiscoverAll
	if solanaOnly {
		items = x402.FilterSolanaCompatible(items)
	}

	if query != "" {
		items = x402.MatchKeyword(items, query)
	}

	output.FormatX402Discover(items, dr.Pagination.Total, query, solanaOnly,
		x402DiscoverLimit, x402DiscoverOffset)
}

// headerHowToFix is the advice for a --header that is not "Name: value".
const headerHowToFix = "Pass each header as --header \"Name: value\", e.g. --header \"Accept: application/json\""

// parseHeaders reads --header values ("Name: value"). One without a colon
// or a name is an error, not dropped: the API could otherwise be paid for a
// request that lacks a header it needs.
func parseHeaders(raw []string) (map[string]string, error) {
	headers := make(map[string]string)
	for _, h := range raw {
		name, value, ok := strings.Cut(h, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --header %q", h)
		}
		headers[name] = strings.TrimSpace(value)
	}
	return headers, nil
}

func isJSON(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "json")
}

func parseJSONBody(body string) interface{} {
	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return body
	}
	return parsed
}

func init() {
	x402FetchCmd.Flags().StringVar(&x402FetchMethod, "method", "GET", "HTTP method (GET, POST, PUT, etc.)")
	x402FetchCmd.Flags().StringVar(&x402FetchBody, "body", "", "Request body for POST/PUT requests")
	x402FetchCmd.Flags().StringArrayVar(&x402FetchHeaders, "header", nil, "Custom header (repeatable, format: \"Key: Value\")")

	// fetch confirm flags (for forwarding body/headers to the paid request)
	x402FetchConfirmCmd.Flags().StringVar(&x402ConfirmMethod, "method", "", "Override HTTP method for the paid request")
	x402FetchConfirmCmd.Flags().StringVar(&x402ConfirmBody, "body", "", "Request body for POST/PUT (same as original fetch)")
	x402FetchConfirmCmd.Flags().StringArrayVar(&x402ConfirmHeaders, "header", nil, "Custom header for paid request (repeatable, format: \"Key: Value\")")

	x402FetchCmd.AddCommand(x402FetchConfirmCmd)

	// discover flags
	x402DiscoverCmd.Flags().BoolVar(&x402DiscoverBazaar, "bazaar", false, "Search the full x402 Bazaar (Coinbase CDP) instead of curated catalog")
	x402DiscoverCmd.Flags().IntVar(&x402DiscoverLimit, "limit", 20, "Max results (bazaar mode only)")
	x402DiscoverCmd.Flags().IntVar(&x402DiscoverOffset, "offset", 0, "Pagination offset (bazaar mode only)")
	x402DiscoverCmd.Flags().BoolVar(&x402DiscoverAll, "all", false, "Show all networks (bazaar mode only, default: Solana-compatible)")
	x402DiscoverCmd.Flags().StringVar(&x402DiscoverFacilitator, "facilitator", "", "Override facilitator URL (or set X402_FACILITATOR_URL)")

	// Register under x402
	x402Cmd.AddCommand(x402FetchCmd)
	x402Cmd.AddCommand(x402DiscoverCmd)
}
