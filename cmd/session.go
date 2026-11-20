package cmd

import (
	"fmt"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
)

// =============================================================================
// The Wallet a Command Acts As, and its API Client
// =============================================================================

// currentWallet returns the wallet this command acts as (see
// config.ResolveWallet). If --wallet is unknown or conflicts with the API
// key, it prints the error and exits.
func currentWallet() *config.ResolvedWallet {
	w, err := config.ResolveWallet(apiKeyFlag, walletFlag)
	if err != nil {
		exitWalletError(err)
	}
	return w
}

// authedWallet returns the wallet this command acts as and an API client
// with its API key. Without an API key it prints the error and exits.
func authedWallet() (*config.ResolvedWallet, *api.Client) {
	w := currentWallet()
	requireAPIKey(w)
	return w, newClient(w.APIKey)
}

// authedClient is authedWallet for a command that needs only the client.
func authedClient() *api.Client {
	_, client := authedWallet()
	return client
}

// requireAPIKey prints an error and exits unless w has an API key.
func requireAPIKey(w *config.ResolvedWallet) {
	if w.APIKey != "" {
		return
	}
	if w.ConfigErr != nil {
		exitWalletError(w.ConfigErr)
		return
	}

	howToFix := "Set BOTWALLET_API_KEY environment variable, use --api-key flag, or create a wallet"
	if names, _ := config.WalletNames(); len(names) > 0 {
		howToFix = fmt.Sprintf("Use --wallet flag to select a wallet. Available: %v", names)
	}

	exitError(
		"NO_API_KEY",
		"API key required",
		howToFix,
		map[string]interface{}{
			"config_path": config.ConfigPath(),
			"example":     "botwallet --api-key bw_bot_xxx wallet balance",
			"env_var":     "BOTWALLET_API_KEY",
			"no_wallet":   "If you don't have a wallet yet, run: botwallet wallet create --name \"YourBotName\"",
		},
	)
}

// newClient returns an API client that sends apiKey ("" for none) to the
// API base URL in use (config.GetBaseURL).
func newClient(apiKey string) *api.Client {
	return api.New(apiKey, config.GetBaseURL(baseURLFlag))
}

// anonClient returns an API client that sends no API key, for the calls
// the server answers without one (wallet creation, wallet_import_key, the
// x402 catalog).
func anonClient() *api.Client {
	return newClient("")
}
