package solana

// USDC mints Botwallet pays with, one per Solana cluster. They match the
// server's constants, so a payment option or a transaction in any other
// token is not a Botwallet payment.
const (
	USDCMintMainnet = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	USDCMintDevnet  = "Gh9ZwEmdLJ8DscKNTkTqPbNwLNNBjuSzaG9Vp2KGtKJr"

	// USDCDecimals is the number of decimals of both mints: 1 USDC is
	// 1,000,000 base units.
	USDCDecimals = 6
)

// Cluster names as the Botwallet API reports them in "network".
const (
	ClusterMainnet = "mainnet-beta"
	ClusterDevnet  = "devnet"
)

// USDCMint returns the USDC mint for a cluster name as the Botwallet API
// reports it. An empty name is mainnet, the only cluster production runs on.
func USDCMint(cluster string) (string, bool) {
	switch cluster {
	case ClusterMainnet, "":
		return USDCMintMainnet, true
	case ClusterDevnet:
		return USDCMintDevnet, true
	}
	return "", false
}
