package txcheck

import (
	"errors"
	"fmt"

	solanago "github.com/gagliardetto/solana-go"
)

// message is a decoded legacy Solana message.
type message struct {
	numRequiredSignatures uint8
	numReadonlySigned     uint8
	numReadonlyUnsigned   uint8
	accountKeys           []solanago.PublicKey
	instructions          []instruction
}

// instruction is a compiled instruction: indexes into accountKeys.
type instruction struct {
	programIndex uint8
	accounts     []uint8
	data         []byte
}

// program returns the instruction's program ID.
func (m *message) program(ix instruction) solanago.PublicKey {
	return m.accountKeys[ix.programIndex]
}

// account returns the key of the instruction's i-th account.
func (m *message) account(ix instruction, i int) solanago.PublicKey {
	return m.accountKeys[ix.accounts[i]]
}

// parseMessage decodes a legacy message:
//
//	header (3 bytes) | compact-u16 n + n*32 account keys | 32-byte blockhash |
//	compact-u16 n + n*(program index, compact-u16 n + account indexes,
//	compact-u16 n + data)
//
// Versioned (v0) messages, which can pull accounts from lookup tables, are
// refused: the server never builds them.
func parseMessage(b []byte) (*message, error) {
	r := reader{buf: b}
	m := &message{}

	header, err := r.bytes(3)
	if err != nil {
		return nil, err
	}
	if header[0]&0x80 != 0 {
		return nil, errors.New("it is a versioned transaction")
	}
	m.numRequiredSignatures, m.numReadonlySigned, m.numReadonlyUnsigned = header[0], header[1], header[2]

	numKeys, err := r.compactU16()
	if err != nil {
		return nil, err
	}
	seen := make(map[solanago.PublicKey]bool, numKeys)
	for i := 0; i < numKeys; i++ {
		raw, err := r.bytes(32)
		if err != nil {
			return nil, err
		}
		key := solanago.PublicKeyFromBytes(raw)
		if seen[key] {
			return nil, fmt.Errorf("account %s is listed twice", key)
		}
		seen[key] = true
		m.accountKeys = append(m.accountKeys, key)
	}
	if m.numRequiredSignatures == 0 || int(m.numRequiredSignatures)+int(m.numReadonlyUnsigned) > numKeys ||
		m.numReadonlySigned >= m.numRequiredSignatures {
		return nil, errors.New("its header does not match its accounts")
	}

	if _, err := r.bytes(32); err != nil { // recent blockhash
		return nil, err
	}

	numInstructions, err := r.compactU16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < numInstructions; i++ {
		var ix instruction
		p, err := r.bytes(1)
		if err != nil {
			return nil, err
		}
		ix.programIndex = p[0]
		if int(ix.programIndex) >= numKeys {
			return nil, errors.New("an instruction points at a missing program")
		}
		n, err := r.compactU16()
		if err != nil {
			return nil, err
		}
		if ix.accounts, err = r.bytes(n); err != nil {
			return nil, err
		}
		for _, a := range ix.accounts {
			if int(a) >= numKeys {
				return nil, errors.New("an instruction points at a missing account")
			}
		}
		if n, err = r.compactU16(); err != nil {
			return nil, err
		}
		if ix.data, err = r.bytes(n); err != nil {
			return nil, err
		}
		m.instructions = append(m.instructions, ix)
	}

	if r.off != len(b) {
		return nil, errors.New("it has extra bytes after the instructions")
	}
	return m, nil
}

// reader walks a message buffer.
type reader struct {
	buf []byte
	off int
}

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.off+n > len(r.buf) {
		return nil, errors.New("it is cut short")
	}
	b := r.buf[r.off : r.off+n]
	r.off += n
	return b, nil
}

// compactU16 reads Solana's 1-3 byte little-endian varint, in its shortest
// form only (as Solana itself reads it).
func (r *reader) compactU16() (int, error) {
	v := 0
	for i := 0; i < 3; i++ {
		b, err := r.bytes(1)
		if err != nil {
			return 0, err
		}
		v |= int(b[0]&0x7f) << (7 * i)
		if b[0]&0x80 == 0 {
			if v > 0xffff || (i > 0 && b[0] == 0) {
				break
			}
			return v, nil
		}
	}
	return 0, errors.New("it has a malformed length")
}
