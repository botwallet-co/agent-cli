// =============================================================================
// x402 Curated API Catalog
// =============================================================================
// Queries Botwallet's curated catalog of verified x402 APIs via the
// public API endpoint. Only contains APIs tested and confirmed on Solana.
// =============================================================================

package x402

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// CatalogEntry represents a single API in our curated catalog.
type CatalogEntry struct {
	ID          string                 `json:"id"`
	Slug        string                 `json:"slug"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	URL         string                 `json:"url"`
	Hostname    string                 `json:"hostname"`
	Method      string                 `json:"method"`
	Network     string                 `json:"network"`
	Networks    []string               `json:"networks"`
	PriceUSDC   float64                `json:"price_usdc"`
	PriceRaw    string                 `json:"price_raw"`
	Category    string                 `json:"category"`
	Source      string                 `json:"source"`
	VerifiedAt  string                 `json:"verified_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type catalogResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Entries []CatalogEntry `json:"entries"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// DiscoverCatalog queries Botwallet's curated x402 API catalog via the public endpoint.
func DiscoverCatalog(apiBaseURL string, query string) ([]CatalogEntry, error) {
	endpoint := apiBaseURL + "/public?action=x402_catalog"

	if query != "" {
		endpoint += "&query=" + url.QueryEscape(query)
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Botwallet-CLI/x402")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("catalog returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxDiscoveryBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog response: %w", err)
	}

	var result catalogResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse catalog: %w", err)
	}

	if !result.Success {
		msg := "unknown error"
		if result.Error != nil {
			msg = result.Error.Message
		}
		return nil, fmt.Errorf("catalog error: %s", msg)
	}

	return result.Data.Entries, nil
}
