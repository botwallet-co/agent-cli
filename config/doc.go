// =============================================================================
// Botwallet CLI Configuration - Multi-Wallet Support
// =============================================================================
// Handles secure storage of multiple wallet credentials:
// - API keys stored in config.json (0600 permissions)
// - Key shares (S1) stored separately in seeds/ folder (0600 permissions)
// - Key shares are NEVER printed to stdout during normal operation
//
// Directory structure (set BOTWALLET_HOME to use another folder):
//   ~/.botwallet/
//   ├── config.json          # Wallet registry + API keys (0600)
//   ├── config.lock          # Exists only while a command updates config.json (lock.go)
//   ├── pending-*.json       # Only if a new wallet could not be added to config.json
//   ├── .backup-nonce        # Temporary backup nonce (auto-deleted)
//   └── seeds/               # Key share folder (0700)
//       ├── my-bot.seed      # Individual key share files (0600, never overwritten)
//       ├── test-bot.seed
//       └── unregistered-<address>.seed  # Key 1 of a create whose outcome was unknown
//
// API Key Priority (see ResolveWallet):
// 1. --api-key flag (highest priority)
// 2. BOTWALLET_API_KEY (or BW_API_KEY) environment variable
// 3. --wallet flag (selects wallet from config)
// 4. Default wallet from config file
// --wallet together with a key from 1 or 2 must name the same wallet.
//
// Files: paths.go (the folder), store.go (config.json), seeds.go (Key 1
// files), newwallet.go (storing a new wallet), wallets.go (listing and
// editing wallets), resolve.go (which wallet a command acts as), backup.go
// (the backup confirmation code), lock.go (lock and safe writes), bwlt.go
// and crypto.go (.bwlt transfer files).
// =============================================================================

package config
