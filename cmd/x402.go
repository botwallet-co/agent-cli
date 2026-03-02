package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
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
  discover  Search x402 Bazaar for paid APIs`,
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
1. Makes a direct HTTP request to the URL (no payment)
2. If the API is free (200), returns the data immediately
3. If the API requires payment (402), parses the price and:
   - Checks your guard rails (auto-approve, limits, budget)
   - Creates a payment intent on the server
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
		output.ValidationError(fmt.Sprintf("Invalid URL: %s", err), "Provide a valid public HTTP(S) URL")
		return
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Probing %s ...", targetURL)
	}

	headers := parseHeaders(x402FetchHeaders)

	resp, err := x402.Fetch(targetURL, x402FetchMethod, headers, x402FetchBody)
	if err != nil {
		output.APIError("FETCH_FAILED", fmt.Sprintf("Failed to reach %s: %s", targetURL, err), "Check the URL and try again", nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		body, readErr := x402.ReadResponseBody(resp)
		if readErr != nil && body == "" {
			output.APIError("READ_FAILED", readErr.Error(), "The API response could not be read", nil)
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

		if !output.IsHumanOutput() {
			output.JSON(result)
		} else {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				output.SuccessMsg("API returned data (free, no payment required)")
			} else {
				output.WarningMsg("API returned HTTP %d (no payment required)", resp.StatusCode)
			}
			output.KeyValue("URL", targetURL)
			output.KeyValue("Status", resp.StatusCode)
			if body != "" {
				fmt.Println()
				fmt.Println(body)
			}
		}
		return
	}

	// 402 Payment Required — parse x402 response
	pr, err := x402.Parse402Response(resp)
	if err != nil {
		output.APIError("X402_PARSE_ERROR", fmt.Sprintf("Could not parse 402 response: %s", err),
			"The API may not implement the x402 standard correctly", nil)
		return
	}

	// Find Solana-compatible payment option
	solOpt := x402.FindSolanaOption(pr)
	if solOpt == nil {
		networks := x402.AvailableNetworks(pr)
		result := map[string]interface{}{
			"payment_required":   true,
			"url":                targetURL,
			"compatible":         false,
			"reason":             "No Solana payment option available. Your wallet supports Solana only.",
			"available_networks": networks,
			"options":            x402.AllSummaries(pr),
		}
		if !output.IsHumanOutput() {
			output.JSON(result)
		} else {
			output.WarningMsg("API requires payment but no Solana option is available")
			output.KeyValue("Available networks", strings.Join(networks, ", "))
			output.Tip("Your Botwallet supports Solana USDC payments only.")
		}
		return
	}

	if !requireAPIKey() {
		return
	}
	client := getClient()

	serverResult, err := client.X402Prepare(
		targetURL,
		solOpt.PayTo,
		solOpt.GetAmount(),
		x402.NormalizeSolanaNetwork(solOpt.Network),
		x402FetchMethod,
	)
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
2. Builds a USDC transfer transaction
3. FROST threshold signs it (agent + server cooperate)
4. Sends the signed payment to the external API
5. Returns the API response data

If the original probe used --method, --body, or --header, pass them
again here so the paid request matches what the API expects.

Only works for intents with status:
- PRE-APPROVED (guard rails passed)
- APPROVED (human owner approved)

Payment intents expire after 48 hours.`,
	Example: `  # Simple GET:
  botwallet x402 fetch confirm <fetch_id>

  # POST with body (same flags as the original fetch):
  botwallet x402 fetch confirm <fetch_id> --method POST --body '{"q":"test"}'`,
	Args: cobra.ExactArgs(1),
	Run:  runX402FetchConfirm,
}

func runX402FetchConfirm(cmd *cobra.Command, args []string) {
	fetchID := args[0]

	if !requireAPIKey() {
		return
	}

	client := getClient()

	if output.IsHumanOutput() {
		output.InfoMsg("Confirming x402 payment %s...", fetchID)
	}

	// Step 2a: Server builds Solana transaction
	confirmResult, err := client.X402Confirm(fetchID)
	if err != nil {
		handleAPIError(err)
		return
	}

	messageB64, ok := confirmResult["message"].(string)
	if !ok || messageB64 == "" {
		output.APIError("SIGNING_ERROR", "Server did not return a message to sign",
			"The payment intent may have expired. Run 'x402 fetch' again", nil)
		return
	}
	transactionID, ok := confirmResult["transaction_id"].(string)
	if !ok || transactionID == "" {
		output.APIError("SIGNING_ERROR", "Server did not return a transaction_id",
			"The payment intent may have expired. Run 'x402 fetch' again", nil)
		return
	}

	if output.IsHumanOutput() {
		toAddr, _ := confirmResult["to_address"].(string)
		amountUSDC, _ := confirmResult["amount_usdc"].(float64)
		feeUSDC, _ := confirmResult["fee_usdc"].(float64)
		totalUSDC, _ := confirmResult["total_usdc"].(float64)
		apiURL, _ := confirmResult["url"].(string)

		summary := fmt.Sprintf("API:    %s\nTo:     %s\nAmount: $%.2f USDC\nFee:    $%.2f USDC\nTotal:  $%.2f USDC",
			apiURL, truncateAddr(toAddr), amountUSDC, feeUSDC, totalUSDC)
		output.Box("x402 Payment", summary)
		fmt.Println()
	}

	// Step 2b: FROST threshold signing (returns signed tx, does NOT submit to Solana)
	signResult, err := frostSignForX402(client, transactionID, messageB64, walletFlag)
	if err != nil {
		output.APIError("SIGNING_ERROR", err.Error(),
			"Check your wallet configuration and try again", nil)
		return
	}

	signedTxB64, ok := signResult["signed_transaction"].(string)
	if !ok || signedTxB64 == "" {
		output.APIError("SIGNING_ERROR", "Server did not return a signed transaction",
			"The signing may have failed. Try again with 'x402 fetch'", nil)
		return
	}

	targetURL, _ := confirmResult["url"].(string)
	if targetURL == "" {
		output.APIError("X402_ERROR", "Server did not return the target URL",
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

	network, _ := confirmResult["network"].(string)

	xPayment, err := x402.BuildXPaymentHeader(signedTxB64, network)
	if err != nil {
		output.APIError("X402_ERROR", fmt.Sprintf("Failed to build payment header: %v", err),
			"This is unexpected. Try again", nil)
		return
	}

	paidHeaders := parseHeaders(x402ConfirmHeaders)

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
		output.APIError("X402_FETCH_FAILED", fmt.Sprintf("Failed to reach API with payment: %s", err),
			"The payment transaction may still settle on-chain. Check your balance.", details)
		return
	}
	defer apiResp.Body.Close()

	body, readErr := x402.ReadResponseBody(apiResp)
	if readErr != nil && body == "" {
		body = fmt.Sprintf("(failed to read response: %v)", readErr)
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
		if !output.IsHumanOutput() {
			output.JSON(result)
		} else {
			output.ErrorMsg("API returned error %d after payment was sent", apiResp.StatusCode)
			if body != "" {
				fmt.Println(body)
			}
		}
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

	baseURL := config.GetBaseURL(baseURLFlag)
	if baseURL == "" {
		baseURL = api.DefaultBaseURL
	}

	entries, err := x402.DiscoverCatalog(baseURL, query)
	if err != nil {
		output.APIError("CATALOG_FAILED",
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
		facilitatorURL = x402.DefaultFacilitatorURL()
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Querying x402 Bazaar (Coinbase CDP facilitator)...")
	}

	dr, err := x402.DiscoverAPIs(facilitatorURL, x402DiscoverLimit, x402DiscoverOffset)
	if err != nil {
		output.APIError("DISCOVERY_FAILED",
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

func parseHeaders(raw []string) map[string]string {
	headers := make(map[string]string)
	for _, h := range raw {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return headers
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

func truncateAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-8:]
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
