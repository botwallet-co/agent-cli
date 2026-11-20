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
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/botwallet-co/agent-cli/solana"
)

const (
	// DefaultFacilitatorURL is the x402 facilitator queried when neither
	// --facilitator nor X402_FACILITATOR_URL names another.
	DefaultFacilitatorURL = "https://api.cdp.coinbase.com/platform/v2/x402"

	maxDiscoveryBody = 5 * 1024 * 1024 // 5 MB cap for discovery responses
)

// DiscoverAPIs queries the facilitator's discovery endpoint and returns
// the raw catalog of x402-enabled resources.
func DiscoverAPIs(facilitatorURL string, limit, offset int) (*DiscoveryResponse, error) {
	endpoint := fmt.Sprintf("%s/discovery/resources?type=http&limit=%d&offset=%d",
		strings.TrimRight(facilitatorURL, "/"), limit, offset)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := facilitatorClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach facilitator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("facilitator returned %d: %s", resp.StatusCode, string(body))
	}

	data, truncated, err := readCapped(resp.Body, maxDiscoveryBody)
	if err != nil {
		return nil, fmt.Errorf("failed to read discovery response: %w", err)
	}
	if truncated {
		return nil, fmt.Errorf("discovery response exceeds %d bytes", maxDiscoveryBody)
	}

	var dr DiscoveryResponse
	if err := json.Unmarshal(data, &dr); err != nil {
		return nil, fmt.Errorf("failed to parse discovery response: %w", err)
	}

	return &dr, nil
}

// Solana clusters Botwallet pays on, and their CAIP-2 ids (genesis hash
// prefixes), lowercased.
const (
	clusterMainnet = "mainnet"
	clusterDevnet  = "devnet"

	caip2SolanaMainnet = "solana:5eykt4usfv8p8njdtrepy1vzqkqzkvdp"
	caip2SolanaDevnet  = "solana:etwtrabzayq6imfeykouru166vu2xqa1"
)

// solanaCluster returns clusterMainnet or clusterDevnet for a network name
// as x402 writes it, or "" for any other network (including other Solana
// clusters such as testnet).
func solanaCluster(network string) string {
	net := strings.ToLower(network)
	switch {
	case net == caip2SolanaDevnet || (strings.HasPrefix(net, "solana") && strings.Contains(net, "devnet")):
		return clusterDevnet
	case net == "solana" || net == "solana-mainnet" || net == caip2SolanaMainnet:
		return clusterMainnet
	}
	return ""
}

// NormalizeSolanaNetwork converts a network Botwallet pays on (see
// IsPayableOption) to the name the server takes: "solana" for mainnet
// (also as a CAIP-2 id), "solana-devnet" for devnet. Any other network is
// returned unchanged.
func NormalizeSolanaNetwork(network string) string {
	switch solanaCluster(network) {
	case clusterMainnet:
		return "solana"
	case clusterDevnet:
		return "solana-devnet"
	}
	return network
}

// FilterSolanaCompatible returns only resources that have at least one
// payment option Botwallet can pay (USDC on Solana, see IsPayableOption).
func FilterSolanaCompatible(items []DiscoveredResource) []DiscoveredResource {
	var result []DiscoveredResource
	for i := range items {
		if HasSolanaOption(&items[i]) {
			result = append(result, items[i])
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
		if matchesKeyword(item, q) {
			result = append(result, item)
		}
	}
	return result
}

// matchesKeyword reports whether the lowercased query q is in item's URL,
// its description or a key of its metadata.output.example.
func matchesKeyword(item DiscoveredResource, q string) bool {
	if strings.Contains(strings.ToLower(item.Resource), q) {
		return true
	}
	if desc, ok := item.Metadata["description"].(string); ok && strings.Contains(strings.ToLower(desc), q) {
		return true
	}
	if out, ok := item.Metadata["output"].(map[string]interface{}); ok {
		if ex, ok := out["example"].(map[string]interface{}); ok {
			for k := range ex {
				if strings.Contains(strings.ToLower(k), q) {
					return true
				}
			}
		}
	}
	return false
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

// ResourceBestPrice returns the price of the first option Botwallet can pay
// (USDC on Solana), else of the first option. Returns empty string if no
// price available.
func ResourceBestPrice(item *DiscoveredResource) (price string, network string) {
	for i := range item.Accepts {
		if opt := &item.Accepts[i]; IsPayableOption(opt) {
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
	return val / math.Pow10(solana.USDCDecimals)
}

// HasSolanaOption returns true if the resource accepts a payment Botwallet
// can make: USDC on Solana (IsPayableOption).
func HasSolanaOption(item *DiscoveredResource) bool {
	for i := range item.Accepts {
		if IsPayableOption(&item.Accepts[i]) {
			return true
		}
	}
	return false
}
