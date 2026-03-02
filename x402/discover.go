// =============================================================================
// x402 Facilitator Discovery Client
// =============================================================================
// Queries the x402 facilitator's /discovery/resources endpoint to find
// x402-enabled APIs. This is a direct external HTTP call — the Botwallet
// server is not involved in discovery.
//
// Default: https://api.cdp.coinbase.com/platform/v2/x402 (Coinbase CDP)
// =============================================================================

package x402

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultFacilitatorURL = "https://api.cdp.coinbase.com/platform/v2/x402"
	maxDiscoveryBody      = 5 * 1024 * 1024 // 5 MB cap for discovery responses
)

// DefaultFacilitatorURL returns the default x402 facilitator base URL.
func DefaultFacilitatorURL() string {
	return defaultFacilitatorURL
}

// DiscoverAPIs queries the facilitator's discovery endpoint and returns
// the raw catalog of x402-enabled resources.
func DiscoverAPIs(facilitatorURL string, limit, offset int) (*DiscoveryResponse, error) {
	if facilitatorURL == "" {
		facilitatorURL = defaultFacilitatorURL
	}

	endpoint := fmt.Sprintf("%s/discovery/resources?type=http&limit=%d&offset=%d",
		strings.TrimRight(facilitatorURL, "/"), limit, offset)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Botwallet-CLI/x402")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach facilitator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("facilitator returned %d: %s", resp.StatusCode, string(body))
	}

	limited := io.LimitReader(resp.Body, int64(maxDiscoveryBody)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read discovery response: %w", err)
	}
	if len(data) > maxDiscoveryBody {
		return nil, fmt.Errorf("discovery response exceeds %d bytes", maxDiscoveryBody)
	}

	var dr DiscoveryResponse
	if err := json.Unmarshal(data, &dr); err != nil {
		return nil, fmt.Errorf("failed to parse discovery response: %w", err)
	}

	return &dr, nil
}

// IsSolanaNetwork returns true if the network string identifies a Solana network.
// Supports both short names ("solana") and CAIP-2 format ("solana:5eykt4U...").
func IsSolanaNetwork(network string) bool {
	net := strings.ToLower(network)
	return net == "solana" || net == "solana-mainnet" || net == "solana-devnet" ||
		strings.HasPrefix(net, "solana:")
}

// NormalizeSolanaNetwork converts CAIP-2 format (solana:5eykt4U...) to "solana"
// for server-side compatibility, preserving devnet if specified.
func NormalizeSolanaNetwork(network string) string {
	net := strings.ToLower(network)
	if net == "solana-devnet" || strings.Contains(net, "devnet") {
		return "solana-devnet"
	}
	if IsSolanaNetwork(network) {
		return "solana"
	}
	return network
}

// FilterSolanaCompatible returns only resources that have at least one
// Solana-compatible payment option.
func FilterSolanaCompatible(items []DiscoveredResource) []DiscoveredResource {
	var result []DiscoveredResource
	for _, item := range items {
		for _, opt := range item.Accepts {
			if IsSolanaNetwork(opt.Network) {
				result = append(result, item)
				break
			}
		}
	}
	return result
}

// MatchKeyword filters resources by case-insensitive substring match
// on the resource URL and metadata description.
func MatchKeyword(items []DiscoveredResource, query string) []DiscoveredResource {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	var result []DiscoveredResource
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Resource), q) {
			result = append(result, item)
			continue
		}
		if desc, ok := item.Metadata["description"].(string); ok {
			if strings.Contains(strings.ToLower(desc), q) {
				result = append(result, item)
				continue
			}
		}
		// Check nested metadata.output.example keys
		if out, ok := item.Metadata["output"].(map[string]interface{}); ok {
			if ex, ok := out["example"].(map[string]interface{}); ok {
				for k := range ex {
					if strings.Contains(strings.ToLower(k), q) {
						result = append(result, item)
						goto next
					}
				}
			}
		}
	next:
	}
	return result
}

// ResourceDescription extracts a human-readable description from a
// discovered resource's metadata, falling back to the URL itself.
func ResourceDescription(item *DiscoveredResource) string {
	if item.Metadata != nil {
		if desc, ok := item.Metadata["description"].(string); ok && desc != "" {
			return desc
		}
	}
	return item.Resource
}

// ResourceBestPrice returns the lowest price across all payment options
// for a discovered resource, preferring Solana options. Returns empty
// string if no price available.
func ResourceBestPrice(item *DiscoveredResource) (price string, network string) {
	for _, opt := range item.Accepts {
		if IsSolanaNetwork(opt.Network) {
			return opt.GetAmount(), opt.Network
		}
	}
	if len(item.Accepts) > 0 {
		return item.Accepts[0].GetAmount(), item.Accepts[0].Network
	}
	return "", ""
}

// ResourceBestPriceUSDC is like ResourceBestPrice but converts the raw
// smallest-unit amount to a human-readable USDC decimal (6 decimals).
func ResourceBestPriceUSDC(item *DiscoveredResource) (priceUSDC float64, network string) {
	raw, net := ResourceBestPrice(item)
	if raw == "" {
		return 0, net
	}
	return RawAmountToUSDC(raw), net
}

// RawAmountToUSDC converts a raw token amount string (smallest unit) to
// a USDC decimal value. USDC uses 6 decimals on all supported chains.
func RawAmountToUSDC(raw string) float64 {
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return val / 1_000_000
}

// HasSolanaOption returns true if the resource accepts payment on Solana.
func HasSolanaOption(item *DiscoveredResource) bool {
	for _, opt := range item.Accepts {
		if IsSolanaNetwork(opt.Network) {
			return true
		}
	}
	return false
}
