// =============================================================================
// x402 Response Parser
// =============================================================================
// Parses HTTP 402 responses per the x402 standard and extracts Solana-
// compatible payment options for Botwallet agents.
// =============================================================================

package x402

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Parse402Response extracts x402 payment requirements from a 402 HTTP response.
// Supports both formats:
//   - v1: payment options in the JSON response body
//   - v2: base64-encoded JSON in the "payment-required" header (body may be empty)
func Parse402Response(resp *http.Response) (*PaymentRequired, error) {
	if resp.StatusCode != http.StatusPaymentRequired {
		return nil, fmt.Errorf("expected 402, got %d", resp.StatusCode)
	}

	const max402Body = 1024 * 1024

	// Try body first (v1 style)
	limited := io.LimitReader(resp.Body, int64(max402Body)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read 402 body: %w", err)
	}
	if len(body) > max402Body {
		return nil, fmt.Errorf("402 response body exceeds 1 MB — likely not a valid x402 response")
	}

	var pr PaymentRequired
	if len(body) > 2 { // skip empty "{}" or ""
		if err := json.Unmarshal(body, &pr); err == nil && len(pr.Accepts) > 0 {
			return &pr, nil
		}
	}

	// Fallback: check the "payment-required" header (v2 style, base64-encoded JSON)
	headerVal := resp.Header.Get("Payment-Required")
	if headerVal == "" {
		if len(body) > 2 {
			return nil, fmt.Errorf("402 body parsed but contains no payment options, and no payment-required header found")
		}
		return nil, fmt.Errorf("402 response contains no payment options (empty body, no payment-required header)")
	}

	decoded, err := base64.StdEncoding.DecodeString(headerVal)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(headerVal)
		if err != nil {
			return nil, fmt.Errorf("payment-required header is not valid base64: %w", err)
		}
	}

	if err := json.Unmarshal(decoded, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse payment-required header JSON: %w", err)
	}

	if len(pr.Accepts) == 0 {
		return nil, fmt.Errorf("payment-required header parsed but contains no payment options")
	}

	return &pr, nil
}

// FindSolanaOption returns the first Solana-compatible payment option, or nil.
// Supports both short names ("solana") and CAIP-2 format ("solana:5eykt4U...").
func FindSolanaOption(pr *PaymentRequired) *PaymentOption {
	if pr == nil {
		return nil
	}
	for i := range pr.Accepts {
		opt := &pr.Accepts[i]
		if IsSolanaNetwork(opt.Network) {
			return opt
		}
	}
	return nil
}

// AvailableNetworks returns a deduplicated list of networks in the 402 response.
func AvailableNetworks(pr *PaymentRequired) []string {
	if pr == nil {
		return nil
	}
	seen := map[string]bool{}
	var nets []string
	for _, opt := range pr.Accepts {
		net := strings.ToLower(opt.Network)
		if !seen[net] {
			seen[net] = true
			nets = append(nets, opt.Network)
		}
	}
	return nets
}

// ToSummary converts a PaymentOption to a PaymentSummary for agent display.
func ToSummary(opt *PaymentOption) PaymentSummary {
	return PaymentSummary{
		Network:     opt.Network,
		PriceUSDC:   opt.GetAmount(),
		PayTo:       opt.PayTo,
		Description: opt.Description,
	}
}

// AllSummaries converts all payment options to summaries.
func AllSummaries(pr *PaymentRequired) []PaymentSummary {
	if pr == nil {
		return nil
	}
	summaries := make([]PaymentSummary, len(pr.Accepts))
	for i := range pr.Accepts {
		summaries[i] = ToSummary(&pr.Accepts[i])
	}
	return summaries
}

// BuildXPaymentHeader encodes a signed Solana transaction into the
// X-Payment header format per the x402 spec.
// The signed transaction bytes are base64-encoded and wrapped in the
// required JSON envelope.
func BuildXPaymentHeader(signedTxBase64 string, network string) (string, error) {
	if network == "" {
		network = "solana"
	}
	payload := map[string]interface{}{
		"x402Version": 1,
		"scheme":      "exact",
		"network":     network,
		"payload": map[string]string{
			"signature":   signedTxBase64,
			"transaction": signedTxBase64,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to build X-Payment header: %w", err)
	}
	return string(data), nil
}
