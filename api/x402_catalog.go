package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// X402CatalogEntry is one API in Botwallet's curated x402 catalog.
type X402CatalogEntry struct {
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

const (
	// catalogUserAgent is the User-Agent of catalog requests, which have
	// always identified themselves as the CLI's x402 client.
	catalogUserAgent = "Botwallet-CLI/x402"

	maxCatalogBody = 5 * 1024 * 1024
)

type catalogResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Entries []X402CatalogEntry `json:"entries"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// X402Catalog lists Botwallet's curated catalog of verified x402 APIs,
// filtered by query if it is not "". The catalog is public: no API key is
// sent.
func (c *Client) X402Catalog(query string) ([]X402CatalogEntry, error) {
	endpoint := c.BaseURL + "/public?action=x402_catalog"
	if query != "" {
		endpoint += "&query=" + url.QueryEscape(query)
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", catalogUserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("catalog returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBody+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog response: %w", err)
	}
	if len(data) > maxCatalogBody {
		return nil, fmt.Errorf("catalog response exceeds %d bytes", maxCatalogBody)
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
