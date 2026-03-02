// =============================================================================
// x402 Protocol Types
// =============================================================================
// Go structs matching the x402 standard (https://www.x402.org).
// Used to parse 402 Payment Required responses from external APIs
// and facilitator discovery responses.
// =============================================================================

package x402

// PaymentRequired represents the 402 response body per the x402 standard.
type PaymentRequired struct {
	Accepts []PaymentOption `json:"accepts"`
	Error   string          `json:"error,omitempty"`
	Version int             `json:"x402Version,omitempty"`
}

// PaymentOption describes one way the merchant is willing to accept payment.
// Supports both v1 (maxAmountRequired) and v2 (amount) field names.
type PaymentOption struct {
	Scheme                  string                 `json:"scheme"`
	Network                 string                 `json:"network"`
	MaxAmountRequired       string                 `json:"maxAmountRequired,omitempty"`
	Amount                  string                 `json:"amount,omitempty"`
	Resource                string                 `json:"resource,omitempty"`
	Description             string                 `json:"description,omitempty"`
	MimeType                string                 `json:"mimeType,omitempty"`
	PayTo                   string                 `json:"payTo"`
	Asset                   string                 `json:"asset,omitempty"`
	MaxTimeoutSeconds       int                    `json:"maxTimeoutSeconds,omitempty"`
	RequiredDeadlineSeconds int                    `json:"requiredDeadlineSeconds,omitempty"`
	Extra                   map[string]interface{} `json:"extra,omitempty"`
}

// GetAmount returns the price, checking v2 field first then v1 fallback.
func (o *PaymentOption) GetAmount() string {
	if o.Amount != "" {
		return o.Amount
	}
	return o.MaxAmountRequired
}

// ProbeResult is returned by x402 fetch (Step 1) to the agent.
type ProbeResult struct {
	PaymentRequired bool             `json:"payment_required"`
	URL             string           `json:"url"`
	Method          string           `json:"method"`
	Options         []PaymentSummary `json:"options,omitempty"`
	SolanaOption    *PaymentSummary  `json:"solana_option,omitempty"`
	Compatible      bool             `json:"compatible"`
	Reason          string           `json:"reason,omitempty"`

	// Free API: data is returned directly
	FreeResponse *FreeResponse `json:"response,omitempty"`
}

// PaymentSummary is a simplified view of a PaymentOption for agent consumption.
type PaymentSummary struct {
	Network     string `json:"network"`
	PriceUSDC   string `json:"price_usdc"`
	PayTo       string `json:"pay_to"`
	Description string `json:"description,omitempty"`
}

// FreeResponse wraps a non-402 API response (the API was free).
type FreeResponse struct {
	StatusCode  int               `json:"status_code"`
	ContentType string            `json:"content_type,omitempty"`
	Body        string            `json:"body"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// =============================================================================
// Facilitator Discovery Types
// =============================================================================

// DiscoveryResponse is the top-level response from the facilitator
// /discovery/resources endpoint.
type DiscoveryResponse struct {
	X402Version int                  `json:"x402Version"`
	Items       []DiscoveredResource `json:"items"`
	Pagination  DiscoveryPagination  `json:"pagination"`
}

// DiscoveredResource represents a single x402-enabled API endpoint
// returned by the facilitator's discovery catalog.
type DiscoveredResource struct {
	Resource    string                 `json:"resource"`
	Type        string                 `json:"type"`
	X402Version int                    `json:"x402Version"`
	Accepts     []PaymentOption        `json:"accepts"`
	LastUpdated string                 `json:"lastUpdated"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// DiscoveryPagination holds pagination info from the discovery response.
type DiscoveryPagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}
