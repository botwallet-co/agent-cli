// =============================================================================
// Botwallet API Client
// =============================================================================
// HTTP client for communicating with the Botwallet API.
// All CLI commands use this client to make API calls.
// =============================================================================

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Default configuration. Which base URL a command uses is decided by
// config.GetBaseURL.
const (
	DefaultBaseURL = "https://api.botwallet.co/v1"
	DefaultTimeout = 30 * time.Second
	UserAgent      = "Botwallet-CLI"
)

// Client is the Botwallet API client
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Version    string
}

// Version info (set from main via SetVersion)
var clientVersion = "dev"

// SetVersion sets the client version for User-Agent header
func SetVersion(v string) {
	clientVersion = v
}

// New creates an API client for baseURL ("" means DefaultBaseURL). apiKey
// may be "" for the calls that need no authentication.
func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Version: clientVersion,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// =============================================================================
// Request/Response Types
// =============================================================================

// APIError represents an error from the API
type APIError struct {
	Code     string
	Message  string
	HowToFix string
	Details  map[string]interface{}
}

func (e *APIError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// params is a request body under construction. Its setters leave out empty
// and zero values, which the server reads as "not given".
type params map[string]interface{}

// str sets key to v unless v is "".
func (p params) str(key, v string) params {
	if v != "" {
		p[key] = v
	}
	return p
}

// posInt sets key to v if v is positive.
func (p params) posInt(key string, v int) params {
	if v > 0 {
		p[key] = v
	}
	return p
}

// list sets key to v unless v is empty.
func (p params) list(key string, v []string) params {
	if len(v) > 0 {
		p[key] = v
	}
	return p
}

// listParams is the status filter and paging of the list calls.
func listParams(status string, limit, offset int) params {
	return params{}.str("status", status).posInt("limit", limit).posInt("offset", offset)
}

// =============================================================================
// Core API Method
// =============================================================================

// Call makes an API call with the given action and data
func (c *Client) Call(action string, data map[string]interface{}) (map[string]interface{}, error) {
	return c.CallWithIdempotency(action, data, "")
}

// CallWithIdempotency makes an API call with an idempotency key
func (c *Client) CallWithIdempotency(action string, data map[string]interface{}, idempotencyKey string) (map[string]interface{}, error) {
	// Build request body
	body := map[string]interface{}{
		"action": action,
	}

	// Merge data into body (flat structure, not nested under "data")
	for k, v := range data {
		body[k] = v
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", c.BaseURL+"/bot", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", UserAgent, c.Version))
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	// Make request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// V3 response format: { success: true/false, data: {...}, error: { code, message, how_to_fix } }
	successVal, hasSuccess := result["success"]
	if !hasSuccess {
		return nil, fmt.Errorf("unexpected API response: missing 'success' field")
	}

	successBool, ok := successVal.(bool)
	if !ok {
		return nil, fmt.Errorf("unexpected API response: 'success' is not a boolean")
	}

	if !successBool {
		apiErr := &APIError{
			Details: make(map[string]interface{}),
		}
		if errObj, ok := result["error"].(map[string]interface{}); ok {
			if code, ok := errObj["code"].(string); ok {
				apiErr.Code = code
			}
			if msg, ok := errObj["message"].(string); ok {
				apiErr.Message = msg
			}
			if fix, ok := errObj["how_to_fix"].(string); ok {
				apiErr.HowToFix = fix
			}
			for k, v := range errObj {
				if k != "code" && k != "message" && k != "how_to_fix" {
					apiErr.Details[k] = v
				}
			}
		}
		return nil, apiErr
	}

	// Success: unwrap { success: true, data: {...} }
	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}
	return result, nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// Ping tests connectivity to the API
func (c *Client) Ping() (map[string]interface{}, error) {
	return c.Call("ping", nil)
}

// DKGInit starts the FROST Distributed Key Generation protocol (no auth required).
// Returns the server's public key share (A2) and a session ID.
// SECURITY: No secret material is sent or received — only public points.
func (c *Client) DKGInit(name string, agentModel string, ownerEmail string, metadata map[string]interface{}) (map[string]interface{}, error) {
	data := params{"name": name}.str("agent_model", agentModel).str("owner_email", ownerEmail)
	if len(metadata) > 0 {
		data["metadata"] = metadata
	}
	return c.Call("dkg_init", data)
}

// DKGComplete finishes the FROST DKG protocol (no auth required).
// Sends the bot's public key share (A1) and the group public key (A).
// The server verifies A == A1 + A2 and creates the wallet.
// SECURITY: Only public key shares cross the wire. S1 NEVER leaves the CLI.
func (c *Client) DKGComplete(sessionID string, botPublicShare string, groupPublicKey string) (map[string]interface{}, error) {
	return c.Call("dkg_complete", map[string]interface{}{
		"session_id":         sessionID,
		"agent_public_share": botPublicShare,
		"group_public_key":   groupPublicKey,
	})
}

// UpdateName updates the wallet's display name (username/slug is immutable)
func (c *Client) UpdateName(name string) (map[string]interface{}, error) {
	return c.Call("update_name", map[string]interface{}{
		"name": name,
	})
}

// UpdateOwner updates the pledged owner email (only for unclaimed wallets)
func (c *Client) UpdateOwner(ownerEmail string) (map[string]interface{}, error) {
	return c.Call("update_owner", map[string]interface{}{
		"owner_email": ownerEmail,
	})
}

// Info gets wallet information
func (c *Client) Info() (map[string]interface{}, error) {
	return c.Call("info", nil)
}

// Balance gets detailed balance information
func (c *Client) Balance() (map[string]interface{}, error) {
	return c.Call("balance", nil)
}

// Lookup checks if a recipient exists
func (c *Client) Lookup(username string) (map[string]interface{}, error) {
	return c.Call("lookup", map[string]interface{}{
		"username": username,
	})
}

// CanIAfford checks if a payment would succeed
func (c *Client) CanIAfford(to string, amount float64) (map[string]interface{}, error) {
	return c.Call("can_i_afford", map[string]interface{}{
		"to":     to,
		"amount": amount,
	})
}

// Pay sends a payment
func (c *Client) Pay(to string, amount float64, note string, reference string, idempotencyKey string) (map[string]interface{}, error) {
	data := params{"to": to, "amount": amount}.str("note", note).str("reference", reference)
	return c.CallWithIdempotency("pay", data, idempotencyKey)
}

// PayRequest fulfills a payment request
func (c *Client) PayRequest(requestID string, idempotencyKey string) (map[string]interface{}, error) {
	return c.CallWithIdempotency("pay", map[string]interface{}{
		"payment_request_id": requestID,
	}, idempotencyKey)
}

// LineItem represents an invoice line item
type LineItem struct {
	Description    string `json:"description"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	TotalCents     int64  `json:"total_cents"`
}

// PaymentRequest is a paylink to create. Amount, Description and
// RevealOwner are always sent; the rest only when set.
type PaymentRequest struct {
	Amount      float64
	Description string
	Reference   string
	ExpiresIn   string // e.g. "24h", "7d"
	RevealOwner bool   // show the owner's email on the payment page
	LineItems   []LineItem
}

// CreatePaymentRequest creates a new payment request
func (c *Client) CreatePaymentRequest(r PaymentRequest) (map[string]interface{}, error) {
	data := params{
		"amount":       r.Amount,
		"description":  r.Description,
		"reveal_owner": r.RevealOwner,
	}.str("reference", r.Reference).str("expires_in", r.ExpiresIn)
	if len(r.LineItems) > 0 {
		data["line_items"] = r.LineItems
	}
	return c.Call("create_payment_request", data)
}

// SendPaylinkInvitation sends a paylink to an email address or bot wallet
func (c *Client) SendPaylinkInvitation(requestID, toEmail, toWallet, message string) (map[string]interface{}, error) {
	data := params{"request_id": requestID}.str("to_email", toEmail).str("to_wallet", toWallet).str("message", message)
	return c.Call("send_paylink_invitation", data)
}

// GetPaymentRequest gets a payment request by ID or reference
func (c *Client) GetPaymentRequest(requestID string, reference string) (map[string]interface{}, error) {
	return c.Call("get_payment_request", params{}.str("request_id", requestID).str("reference", reference))
}

// ListPaymentRequests lists payment requests
func (c *Client) ListPaymentRequests(status string, limit int, offset int) (map[string]interface{}, error) {
	return c.Call("list_payment_requests", listParams(status, limit, offset))
}

// CancelPaymentRequest cancels a payment request
func (c *Client) CancelPaymentRequest(requestID string) (map[string]interface{}, error) {
	return c.Call("cancel_payment_request", map[string]interface{}{
		"request_id": requestID,
	})
}

// GetDepositAddress gets the deposit address
func (c *Client) GetDepositAddress() (map[string]interface{}, error) {
	return c.Call("get_deposit_address", nil)
}

// RequestFunds asks owner for funds
func (c *Client) RequestFunds(amount float64, reason string) (map[string]interface{}, error) {
	return c.Call("request_funds", map[string]interface{}{
		"amount": amount,
		"reason": reason,
	})
}

// ListFundRequests lists fund requests
func (c *Client) ListFundRequests(status string, limit int, offset int) (map[string]interface{}, error) {
	return c.Call("list_fund_requests", listParams(status, limit, offset))
}

// Withdraw withdraws funds to a Solana address
func (c *Client) Withdraw(amount float64, toAddress string, reason string, idempotencyKey string) (map[string]interface{}, error) {
	return c.CallWithIdempotency("withdraw", map[string]interface{}{
		"amount":     amount,
		"to_address": toAddress,
		"reason":     reason,
	}, idempotencyKey)
}

// ConfirmWithdrawal confirms an approved withdrawal and gets the transaction to sign
func (c *Client) ConfirmWithdrawal(withdrawalID string) (map[string]interface{}, error) {
	return c.Call("confirm_withdrawal", map[string]interface{}{
		"withdrawal_id": withdrawalID,
	})
}

// GetWithdrawal gets a withdrawal status
func (c *Client) GetWithdrawal(withdrawalID string) (map[string]interface{}, error) {
	return c.Call("get_withdrawal", map[string]interface{}{
		"withdrawal_id": withdrawalID,
	})
}

// Transactions gets transaction history. txType is "in", "out", or "" for
// every transaction.
func (c *Client) Transactions(txType string, limit int, offset int) (map[string]interface{}, error) {
	return c.Call("transactions", listParams("", limit, offset).str("type", txType))
}

// MyLimits gets spending limits
func (c *Client) MyLimits() (map[string]interface{}, error) {
	return c.Call("my_limits", nil)
}

// PendingApprovals gets pending approvals
func (c *Client) PendingApprovals() (map[string]interface{}, error) {
	return c.Call("pending_approvals", nil)
}

// ApprovalStatus checks the status of a single approval by ID
func (c *Client) ApprovalStatus(approvalID string) (map[string]interface{}, error) {
	return c.Call("approval_status", map[string]interface{}{
		"approval_id": approvalID,
	})
}

// FrostSignInit starts a FROST signing round (authenticated).
// Sends the bot's nonce commitment (R1) and gets the server's (R2) back.
// SECURITY: Only nonce commitments (public points) cross the wire.
func (c *Client) FrostSignInit(transactionID string, nonceCommitment string) (map[string]interface{}, error) {
	return c.Call("frost_sign_init", map[string]interface{}{
		"transaction_id":   transactionID,
		"nonce_commitment": nonceCommitment,
	})
}

// FrostSignComplete finishes the FROST signing round (authenticated).
// Sends the bot's partial signature. Server aggregates, assembles tx, submits to Solana.
// SECURITY: The partial sig (z1 = r1 + k*s1) cannot reveal S1 without knowing r1.
func (c *Client) FrostSignComplete(sessionID string, partialSig string) (map[string]interface{}, error) {
	return c.Call("frost_sign_complete", map[string]interface{}{
		"session_id":  sessionID,
		"partial_sig": partialSig,
	})
}

// Events fetches recent wallet events/notifications
func (c *Client) Events(types []string, limit int, unreadOnly bool, since string) (map[string]interface{}, error) {
	data := params{"unread_only": unreadOnly}.list("types", types).posInt("limit", limit).str("since", since)
	return c.Call("events", data)
}

// MarkRead marks events as read
func (c *Client) MarkRead(eventIDs []string, all bool) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	if all {
		data["all"] = true
	} else {
		data["event_ids"] = eventIDs
	}
	return c.Call("mark_read", data)
}

// ConfirmPayment confirms a payment and gets the transaction to sign
func (c *Client) ConfirmPayment(transactionID string) (map[string]interface{}, error) {
	return c.Call("confirm_payment", map[string]interface{}{
		"transaction_id": transactionID,
	})
}

// CancelPayment cancels a pending/pre-approved payment
func (c *Client) CancelPayment(transactionID string) (map[string]interface{}, error) {
	return c.Call("cancel_payment", map[string]interface{}{
		"transaction_id": transactionID,
	})
}

// ListPayments lists payment transactions
func (c *Client) ListPayments(transactionID string, status string, limit int, offset int) (map[string]interface{}, error) {
	return c.Call("list_payments", listParams(status, limit, offset).str("transaction_id", transactionID))
}

// ExportWallet requests an export encryption key from the server.
// The server identifies the wallet from the API key (no wallet_id needed).
// Returns the export_id and base64-encoded encryption key.
func (c *Client) ExportWallet() (exportID string, encryptionKeyB64 string, err error) {
	result, err := c.Call("wallet_export", nil)
	if err != nil {
		return "", "", err
	}

	exportID, _ = result["export_id"].(string)
	encryptionKeyB64, _ = result["encryption_key"].(string)

	if exportID == "" || encryptionKeyB64 == "" {
		return "", "", fmt.Errorf("server returned incomplete export data")
	}

	return exportID, encryptionKeyB64, nil
}

// ImportWalletKey retrieves the decryption key for a .bwlt file from the server.
// This is an unauthenticated call (no API key needed).
func (c *Client) ImportWalletKey(exportID string) (encryptionKeyB64 string, err error) {
	result, err := c.Call("wallet_import_key", map[string]interface{}{
		"export_id": exportID,
	})
	if err != nil {
		return "", err
	}

	encryptionKeyB64, _ = result["encryption_key"].(string)
	if encryptionKeyB64 == "" {
		return "", fmt.Errorf("server returned no encryption key")
	}

	return encryptionKeyB64, nil
}

// =============================================================================
// x402 API Methods
// =============================================================================

// X402PrepareRequest is the payment option of a probed x402 API. URL,
// PayTo, Amount, Network and Method are always sent.
type X402PrepareRequest struct {
	URL     string
	PayTo   string
	Amount  string // in base units of Asset
	Network string
	Method  string
	// Asset (the token mint) and Scheme come from the API's payment
	// option; servers that do not know them ignore them.
	Asset  string
	Scheme string
}

// X402Prepare creates an x402 payment intent (Step 1 server call).
// Checks guard rails, balance, and creates a transaction record.
func (c *Client) X402Prepare(r X402PrepareRequest) (map[string]interface{}, error) {
	data := params{
		"url":     r.URL,
		"pay_to":  r.PayTo,
		"amount":  r.Amount,
		"network": r.Network,
		"method":  r.Method,
	}.str("asset", r.Asset).str("scheme", r.Scheme)
	return c.Call("x402_prepare", data)
}

// X402Confirm confirms an x402 payment and builds the Solana transaction (Step 2a).
// Returns the message_to_sign for FROST signing.
func (c *Client) X402Confirm(fetchID string) (map[string]interface{}, error) {
	return c.Call("x402_confirm", map[string]interface{}{
		"fetch_id": fetchID,
	})
}

// X402SignComplete finishes FROST Round 2 for x402 payments.
// Unlike regular frost_sign_complete, this does NOT submit the transaction
// to Solana — it returns the signed transaction bytes for the CLI to include
// in the X-Payment header.
func (c *Client) X402SignComplete(sessionID string, partialSig string) (map[string]interface{}, error) {
	return c.Call("x402_sign_complete", map[string]interface{}{
		"session_id":  sessionID,
		"partial_sig": partialSig,
	})
}

// X402Settle reports the outcome of an x402 API call back to the server.
// Called after the CLI retries the API with payment. Updates the transaction
// status to completed or failed.
func (c *Client) X402Settle(fetchID string, apiSuccess bool, responseStatus int, errorMessage string) (map[string]interface{}, error) {
	data := params{
		"fetch_id":        fetchID,
		"success":         apiSuccess,
		"response_status": responseStatus,
	}.str("error_message", errorMessage)
	return c.Call("x402_settle", data)
}
