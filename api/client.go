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

// Default configuration
//
// API URL can be overridden via:
//   - --api-url flag
//   - BOTWALLET_API_URL environment variable
//   - ~/.botwallet/config.json base_url field
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

// NewClient creates a new API client
func NewClient(apiKey string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		APIKey:  apiKey,
		Version: clientVersion,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// NewClientWithURL creates a new API client with a custom base URL
func NewClientWithURL(apiKey, baseURL string) *Client {
	c := NewClient(apiKey)
	c.BaseURL = baseURL
	return c
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
func (c *Client) DKGInit(name string, agentModel string, ownerEmail string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"name": name,
	}
	if agentModel != "" {
		data["agent_model"] = agentModel
	}
	if ownerEmail != "" {
		data["owner_email"] = ownerEmail
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
	data := map[string]interface{}{
		"to":     to,
		"amount": amount,
	}
	if note != "" {
		data["note"] = note
	}
	if reference != "" {
		data["reference"] = reference
	}
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

// CreatePaymentRequest creates a new payment request
func (c *Client) CreatePaymentRequest(amount float64, description string, reference string, expiresIn string, revealOwner bool, lineItems []LineItem) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"amount":       amount,
		"description":  description,
		"reveal_owner": revealOwner,
	}
	if reference != "" {
		data["reference"] = reference
	}
	if expiresIn != "" {
		data["expires_in"] = expiresIn
	}
	if lineItems != nil && len(lineItems) > 0 {
		data["line_items"] = lineItems
	}
	return c.Call("create_payment_request", data)
}

// SendPaylinkInvitation sends a paylink to an email address or bot wallet
func (c *Client) SendPaylinkInvitation(requestID, toEmail, toWallet, message string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"request_id": requestID,
	}
	if toEmail != "" {
		data["to_email"] = toEmail
	}
	if toWallet != "" {
		data["to_wallet"] = toWallet
	}
	if message != "" {
		data["message"] = message
	}
	return c.Call("send_paylink_invitation", data)
}

// GetPaymentRequest gets a payment request by ID or reference
func (c *Client) GetPaymentRequest(requestID string, reference string) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	if requestID != "" {
		data["request_id"] = requestID
	}
	if reference != "" {
		data["reference"] = reference
	}
	return c.Call("get_payment_request", data)
}

// ListPaymentRequests lists payment requests
func (c *Client) ListPaymentRequests(status string, limit int, offset int) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	if status != "" {
		data["status"] = status
	}
	if limit > 0 {
		data["limit"] = limit
	}
	if offset > 0 {
		data["offset"] = offset
	}
	return c.Call("list_payment_requests", data)
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
	data := map[string]interface{}{}
	if status != "" {
		data["status"] = status
	}
	if limit > 0 {
		data["limit"] = limit
	}
	if offset > 0 {
		data["offset"] = offset
	}
	return c.Call("list_fund_requests", data)
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

// Transactions gets transaction history
func (c *Client) Transactions(txType string, limit int, offset int) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	if txType != "" && txType != "all" {
		data["type"] = txType
	}
	if limit > 0 {
		data["limit"] = limit
	}
	if offset > 0 {
		data["offset"] = offset
	}
	return c.Call("transactions", data)
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
	data := map[string]interface{}{
		"unread_only": unreadOnly,
	}
	if len(types) > 0 {
		data["types"] = types
	}
	if limit > 0 {
		data["limit"] = limit
	}
	if since != "" {
		data["since"] = since
	}
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
	data := map[string]interface{}{}
	if transactionID != "" {
		data["transaction_id"] = transactionID
	}
	if status != "" {
		data["status"] = status
	}
	if limit > 0 {
		data["limit"] = limit
	}
	if offset > 0 {
		data["offset"] = offset
	}
	return c.Call("list_payments", data)
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

// X402Prepare creates an x402 payment intent (Step 1 server call).
// Checks guard rails, balance, and creates a transaction record.
func (c *Client) X402Prepare(url, payTo, amount, network, method string) (map[string]interface{}, error) {
	return c.Call("x402_prepare", map[string]interface{}{
		"url":     url,
		"pay_to":  payTo,
		"amount":  amount,
		"network": network,
		"method":  method,
	})
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
	data := map[string]interface{}{
		"fetch_id":        fetchID,
		"success":         apiSuccess,
		"response_status": responseStatus,
	}
	if errorMessage != "" {
		data["error_message"] = errorMessage
	}
	return c.Call("x402_settle", data)
}
