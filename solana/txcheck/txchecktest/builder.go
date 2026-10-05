// Package txchecktest builds Solana messages byte for byte the way the
// Botwallet server does, for tests of the signing checks. Only tests import it.
//
// BuildPayment follows buildPaymentTransaction in
// supabase/functions/bot/solana-helpers.ts (pay, withdraw and x402 confirm).
// BuildTransfer follows buildTransferTransaction in
// supabase/functions/_shared/solana/transaction.ts (the owner's sign portal),
// which leaves the fee transfer out when there is no fee.
package txchecktest

import (
	"encoding/binary"

	solanago "github.com/gagliardetto/solana-go"
)

// Instruction is a compiled instruction.
type Instruction struct {
	Program  uint8
	Accounts []uint8
	Data     []byte
}

// Message is a legacy Solana message.
type Message struct {
	Header       [3]byte
	Keys         []solanago.PublicKey
	Blockhash    solanago.Hash
	Instructions []Instruction
}

// Index returns the position of key in m.Keys, adding it at the end if it
// is not there yet (the server's addAccount).
func (m *Message) Index(key solanago.PublicKey) uint8 {
	for i, k := range m.Keys {
		if k == key {
			return uint8(i)
		}
	}
	m.Keys = append(m.Keys, key)
	return uint8(len(m.Keys) - 1)
}

// Bytes serializes the message.
func (m *Message) Bytes() []byte {
	out := append([]byte{}, m.Header[:]...)
	out = append(out, compactU16(len(m.Keys))...)
	for _, k := range m.Keys {
		out = append(out, k[:]...)
	}
	out = append(out, m.Blockhash[:]...)
	out = append(out, compactU16(len(m.Instructions))...)
	for _, ix := range m.Instructions {
		out = append(out, ix.Program)
		out = append(out, compactU16(len(ix.Accounts))...)
		out = append(out, ix.Accounts...)
		out = append(out, compactU16(len(ix.Data))...)
		out = append(out, ix.Data...)
	}
	return out
}

// Payment describes one confirm on the server.
type Payment struct {
	FeePayer               solanago.PublicKey
	Wallet                 solanago.PublicKey // sender, the transfer authority
	Source                 solanago.PublicKey // sender's USDC token account
	Recipient              solanago.PublicKey
	RecipientAccount       solanago.PublicKey // recipient's USDC token account
	RecipientAccountExists bool
	FeeCollection          solanago.PublicKey
	FeeAccount             solanago.PublicKey // fee collection's USDC token account
	FeeAccountExists       bool
	Mint                   solanago.PublicKey
	Amount                 uint64
	Fee                    uint64
	Blockhash              solanago.Hash
}

// ATA returns owner's associated token account for mint.
func ATA(owner, mint solanago.PublicKey) solanago.PublicKey {
	ata, _, err := solanago.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		panic(err)
	}
	return ata
}

// BuildPayment builds the message buildPaymentTransaction returns: the fee
// transfer is always there, even for a zero fee.
func BuildPayment(p Payment) *Message {
	return build(p, true)
}

// BuildTransfer builds the message buildTransferTransaction returns: no fee
// transfer (and no fee account) when the fee is zero.
func BuildTransfer(p Payment) *Message {
	return build(p, p.Fee > 0)
}

func build(p Payment, withFee bool) *Message {
	m := &Message{Blockhash: p.Blockhash}
	feePayer := m.Index(p.FeePayer)
	wallet := m.Index(p.Wallet)
	source := m.Index(p.Source)
	dest := m.Index(p.RecipientAccount)
	var feeAccount uint8
	if withFee {
		feeAccount = m.Index(p.FeeAccount)
	}

	needsRecipient := !p.RecipientAccountExists
	needsFee := withFee && !p.FeeAccountExists
	var recipient, feeCollection, mint, system, ataProgram uint8
	if needsRecipient || needsFee {
		if needsRecipient {
			recipient = m.Index(p.Recipient)
		}
		if needsFee {
			feeCollection = m.Index(p.FeeCollection)
		}
		mint = m.Index(p.Mint)
		system = m.Index(solanago.SystemProgramID)
		ataProgram = m.Index(solanago.SPLAssociatedTokenAccountProgramID)
	}
	token := m.Index(solanago.TokenProgramID)

	writable := map[uint8]bool{source: true, dest: true}
	if withFee {
		writable[feeAccount] = true
	}
	delete(writable, feePayer)
	delete(writable, wallet)
	m.Header = [3]byte{2, 0, byte(len(m.Keys) - 2 - len(writable))}

	if needsRecipient {
		m.Instructions = append(m.Instructions, Instruction{ataProgram, []uint8{feePayer, dest, recipient, mint, system, token}, []byte{1}})
	}
	if needsFee {
		m.Instructions = append(m.Instructions, Instruction{ataProgram, []uint8{feePayer, feeAccount, feeCollection, mint, system, token}, []byte{1}})
	}
	m.Instructions = append(m.Instructions, Instruction{token, []uint8{source, dest, wallet}, TransferData(p.Amount)})
	if withFee {
		m.Instructions = append(m.Instructions, Instruction{token, []uint8{source, feeAccount, wallet}, TransferData(p.Fee)})
	}
	return m
}

// TransferData is SPL Token Transfer: tag 3 and the amount as u64 LE.
func TransferData(amount uint64) []byte {
	data := make([]byte, 9)
	data[0] = 3
	binary.LittleEndian.PutUint64(data[1:], amount)
	return data
}

// TransferCheckedData is SPL Token TransferChecked: tag 12, the amount as
// u64 LE, then the decimals.
func TransferCheckedData(amount uint64, decimals uint8) []byte {
	data := make([]byte, 10)
	data[0] = 12
	binary.LittleEndian.PutUint64(data[1:], amount)
	data[9] = decimals
	return data
}

func compactU16(v int) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}
