// Package txcheck decides whether Key 1 may sign a Solana message that the
// Botwallet server prepared. Key 1 is only half of the wallet's signing key,
// so this check is what makes the agent's half an independent approval: the
// CLI signs a plain USDC payment from this wallet to the recipient and amount
// the server showed, plus at most the fee it quoted, and nothing else.
//
// Rules. packages/mcp (src/solana/verify-transaction.ts) and apps/sign
// (src/lib/verify-transaction.ts) apply the same ones, and all three run the
// same test vectors (testdata/vectors.json):
//
//  1. Legacy message (no version prefix), every length in its shortest
//     form, no account listed twice, no bytes after the instructions.
//  2. Exactly 2 signers. Account 0 pays the network fee and is not this
//     wallet; account 1 is this wallet, which may be a read-only signer.
//     "This wallet" is the address in the local config, or the server's
//     group key when no address is on file.
//  3. Only these instructions:
//     a. Associated Token Account Create or CreateIdempotent (data empty, [0]
//     or [1]) with accounts [payer, account, owner, mint, System program,
//     Token program]: payer is account 0, mint is USDC, account is the
//     owner's associated USDC account, the owner is not this wallet, and one
//     of the transfers pays into the account. At most two.
//     b. Token program (not Token-2022) Transfer [source, destination,
//     authority]. The authority is this wallet, and neither the source nor
//     the destination is this wallet's own address.
//     Nothing else: no compute budget, no TransferChecked, no other program.
//     The server builds none of them.
//  4. One or two transfers. The first is the payment: its destination is the
//     recipient's associated USDC account, the only account the server pays
//     into, and its amount is in the expected range, which starts above
//     zero. The second, if any, is the platform fee: at most the quoted fee,
//     from the same token account as the payment. The payment's source is
//     not pinned, because servers before Oct 2026 paid from the first USDC
//     account Solana listed for the wallet. A plain Transfer names no mint,
//     but the Token program refuses one whose source and destination hold
//     different tokens, so the payment's USDC destination makes its source
//     a USDC account, and the fee shares that source. Without this rule the
//     fee slot could move another token the wallet holds (an NFT is a single
//     unit). The fee's destination is not pinned, because the client has no
//     copy of the fee wallet that does not come from the server; the quote
//     caps its cost. It may be the payment's destination, when the recipient
//     is the fee wallet itself.
//  5. When frost_sign_init returns message_to_sign, it is byte for byte the
//     message checked here (the server signs that one).
//
// Expected values come from the confirm response. Pay and withdraw give
// to_address, amount_usdc and fee_usdc in whole cents, and the server moves
// exactly those. x402 gives amount_usdc rounded up to a cent from the API's
// base units, so there the payment can be up to 0.009999 USDC below
// amount_usdc; servers before Oct 2026 also charged their percentage fee on
// the base units, up to 0.01 USDC above fee_usdc. No fee_usdc means no fee.
// "network" picks the USDC mint (mainnet-beta or devnet; anything else is
// refused).
package txcheck

import (
	"encoding/binary"
	"fmt"
	"strings"

	solanago "github.com/gagliardetto/solana-go"
)

// Instruction tags and limits this package accepts.
const (
	tokenTransfer = 3

	maxAccountCreations = 2
)

// Expectation is what the message must do.
type Expectation struct {
	Wallet    solanago.PublicKey // this wallet: the authority of every transfer
	Mint      solanago.PublicKey // USDC mint of the cluster
	Recipient solanago.PublicKey // wallet (owner) address that receives the payment
	MinAmount uint64             // payment amount range, in base units; MinAmount > 0
	MaxAmount uint64
	MaxFee    uint64 // largest platform fee, in base units
}

// Transfer is one USDC transfer out of this wallet.
type Transfer struct {
	Source      solanago.PublicKey
	Destination solanago.PublicKey
	Amount      uint64 // base units
}

// Checked is what a message that passed Verify does.
type Checked struct {
	FeePayer solanago.PublicKey
	Payment  Transfer
	Fee      *Transfer // nil when there is no fee transfer
}

// RefusedError means the message is not one this wallet signs.
type RefusedError struct {
	Reason string
}

func (e *RefusedError) Error() string { return e.Reason }

// Refuse returns a RefusedError with a formatted reason.
func Refuse(format string, args ...interface{}) error {
	return &RefusedError{Reason: fmt.Sprintf(format, args...)}
}

// Verify decodes message and checks it against exp, following the rules in
// the package comment. It returns a *RefusedError when Key 1 must not sign.
func Verify(message []byte, exp Expectation) (*Checked, error) {
	if exp.MaxAmount == 0 {
		return nil, Refuse("the payment shown is for 0 USDC")
	}
	if exp.MinAmount == 0 || exp.MinAmount > exp.MaxAmount {
		return nil, Refuse("the expected payment amount is not valid")
	}
	m, err := parseMessage(message)
	if err != nil {
		return nil, Refuse("the transaction could not be read: %v", err)
	}

	if m.numRequiredSignatures != 2 {
		return nil, Refuse("the transaction needs %d signatures; a Botwallet payment needs two (the network fee payer and this wallet)", m.numRequiredSignatures)
	}
	if m.accountKeys[0] == exp.Wallet {
		return nil, Refuse("the transaction makes this wallet pay the network fee")
	}
	if m.accountKeys[1] != exp.Wallet {
		return nil, Refuse("the transaction is signed by %s, not by this wallet (%s)", m.accountKeys[1], exp.Wallet)
	}

	var transfers []Transfer
	var created []solanago.PublicKey
	for _, ix := range m.instructions {
		switch program := m.program(ix); program {
		case solanago.SPLAssociatedTokenAccountProgramID:
			account, err := checkCreateAccount(m, ix, exp)
			if err != nil {
				return nil, err
			}
			if created = append(created, account); len(created) > maxAccountCreations {
				return nil, Refuse("the transaction creates more than %d token accounts", maxAccountCreations)
			}
		case solanago.TokenProgramID:
			t, err := decodeTransfer(m, ix, exp.Wallet)
			if err != nil {
				return nil, err
			}
			transfers = append(transfers, t)
		default:
			return nil, Refuse("the transaction calls program %s, which a Botwallet payment never uses", program)
		}
	}

	switch len(transfers) {
	case 0:
		return nil, Refuse("the transaction moves no USDC")
	case 1, 2:
	default:
		return nil, Refuse("the transaction makes %d transfers; a Botwallet payment makes at most two (the payment and the fee)", len(transfers))
	}

	payment := transfers[0]
	ata, _, err := solanago.FindAssociatedTokenAddress(exp.Recipient, exp.Mint)
	if err != nil || payment.Destination != ata {
		return nil, Refuse("the payment goes to token account %s, which is not the USDC account of %s", payment.Destination, exp.Recipient)
	}
	if payment.Amount < exp.MinAmount || payment.Amount > exp.MaxAmount {
		if exp.MinAmount == exp.MaxAmount {
			return nil, Refuse("the transaction pays %s USDC, not the %s USDC shown", FormatUSDC(payment.Amount), FormatUSDC(exp.MaxAmount))
		}
		return nil, Refuse("the transaction pays %s USDC, outside the %s-%s USDC shown", FormatUSDC(payment.Amount), FormatUSDC(exp.MinAmount), FormatUSDC(exp.MaxAmount))
	}

	checked := &Checked{FeePayer: m.accountKeys[0], Payment: payment}
	if len(transfers) == 2 {
		fee := transfers[1]
		if fee.Source != payment.Source {
			return nil, Refuse("the transaction takes its fee from token account %s, not from the USDC account the payment comes from (%s)", fee.Source, payment.Source)
		}
		if fee.Amount > exp.MaxFee {
			return nil, Refuse("the transaction charges a %s USDC fee, more than the %s USDC quoted", FormatUSDC(fee.Amount), FormatUSDC(exp.MaxFee))
		}
		checked.Fee = &fee
	}

	for _, account := range created {
		if !paysInto(transfers, account) {
			return nil, Refuse("the transaction creates token account %s, which none of its transfers pays into", account)
		}
	}
	return checked, nil
}

// checkCreateAccount allows an associated token account creation for USDC
// that the network fee payer pays for. It returns the account created.
func checkCreateAccount(m *message, ix instruction, exp Expectation) (solanago.PublicKey, error) {
	var none solanago.PublicKey
	if len(ix.data) > 1 || (len(ix.data) == 1 && ix.data[0] > 1) {
		return none, Refuse("the transaction has a token account instruction other than create")
	}
	if len(ix.accounts) != 6 {
		return none, Refuse("a token account creation in the transaction has %d accounts instead of 6", len(ix.accounts))
	}
	if ix.accounts[0] != 0 {
		return none, Refuse("the transaction makes %s pay for a new token account, not the network fee payer", m.account(ix, 0))
	}
	account, owner, accountMint := m.account(ix, 1), m.account(ix, 2), m.account(ix, 3)
	if accountMint != exp.Mint {
		return none, Refuse("the transaction creates a token account for %s, not for USDC", accountMint)
	}
	if m.account(ix, 4) != solanago.SystemProgramID || m.account(ix, 5) != solanago.TokenProgramID {
		return none, Refuse("a token account creation in the transaction names the wrong programs")
	}
	if owner == exp.Wallet {
		return none, Refuse("the transaction creates a token account for this wallet")
	}
	ata, _, err := solanago.FindAssociatedTokenAddress(owner, exp.Mint)
	if err != nil || ata != account {
		return none, Refuse("the transaction creates token account %s, which is not the USDC account of %s", account, owner)
	}
	return account, nil
}

// decodeTransfer reads a Transfer that wallet authorizes.
func decodeTransfer(m *message, ix instruction, wallet solanago.PublicKey) (Transfer, error) {
	var t Transfer
	if len(ix.data) != 9 || ix.data[0] != tokenTransfer || len(ix.accounts) != 3 {
		tag := -1
		if len(ix.data) > 0 {
			tag = int(ix.data[0])
		}
		return t, Refuse("the transaction has a token instruction other than a transfer (instruction %d)", tag)
	}
	t = Transfer{Source: m.account(ix, 0), Destination: m.account(ix, 1), Amount: binary.LittleEndian.Uint64(ix.data[1:9])}
	if authority := m.account(ix, 2); authority != wallet {
		return t, Refuse("a transfer in the transaction is authorized by %s, not by this wallet", authority)
	}
	if t.Source == wallet || t.Destination == wallet {
		return t, Refuse("a transfer in the transaction uses this wallet's own address as a token account")
	}
	return t, nil
}

// paysInto reports whether one of transfers goes to account.
func paysInto(transfers []Transfer, account solanago.PublicKey) bool {
	for _, t := range transfers {
		if t.Destination == account {
			return true
		}
	}
	return false
}

// FormatUSDC formats base units as USDC with at least two decimals.
func FormatUSDC(units uint64) string {
	s := strings.TrimRight(fmt.Sprintf("%d.%06d", units/1_000_000, units%1_000_000), "0")
	if decimals := len(s) - strings.IndexByte(s, '.') - 1; decimals < 2 {
		s += strings.Repeat("0", 2-decimals)
	}
	return s
}
