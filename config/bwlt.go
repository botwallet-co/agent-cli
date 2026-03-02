package config

import (
	"fmt"
	"os"
)

// .bwlt file format:
//   Bytes 0-3:   Magic "BWLT" (0x42, 0x57, 0x4C, 0x54)
//   Byte  4:     Version (0x01)
//   Bytes 5-7:   Reserved (0x00, 0x00, 0x00)
//   Bytes 8-43:  Export ID (36-byte UUID as ASCII)
//   Bytes 44-55: AES-GCM Nonce (12 bytes)
//   Bytes 56+:   AES-256-GCM ciphertext (JSON payload + 16-byte auth tag)

var bwltMagic = []byte{0x42, 0x57, 0x4C, 0x54} // "BWLT"

const (
	bwltVersion     = 0x01
	bwltHeaderSize  = 8                                                    // magic (4) + version (1) + reserved (3)
	bwltExportIDLen = 36                                                   // UUID string length
	bwltNonceLen    = 12                                                   // AES-GCM nonce
	bwltMinSize     = bwltHeaderSize + bwltExportIDLen + bwltNonceLen + 16 // +16 for minimum GCM auth tag
)

// WriteBWLT writes an encrypted wallet export to a .bwlt file.
func WriteBWLT(path string, exportID string, nonce []byte, ciphertext []byte) error {
	if len(exportID) != bwltExportIDLen {
		return fmt.Errorf("export ID must be %d characters, got %d", bwltExportIDLen, len(exportID))
	}
	if len(nonce) != bwltNonceLen {
		return fmt.Errorf("nonce must be %d bytes, got %d", bwltNonceLen, len(nonce))
	}

	// Build the file contents
	totalSize := bwltHeaderSize + bwltExportIDLen + bwltNonceLen + len(ciphertext)
	buf := make([]byte, 0, totalSize)

	// Header: magic + version + reserved
	buf = append(buf, bwltMagic...)
	buf = append(buf, bwltVersion, 0x00, 0x00, 0x00)

	// Export ID (36-byte UUID ASCII)
	buf = append(buf, []byte(exportID)...)

	// Nonce (12 bytes)
	buf = append(buf, nonce...)

	// Ciphertext (variable length, includes GCM auth tag)
	buf = append(buf, ciphertext...)

	if err := os.WriteFile(path, buf, 0600); err != nil {
		return fmt.Errorf("failed to write .bwlt file: %w", err)
	}

	return nil
}

// ReadBWLT reads a .bwlt file and returns its components.
func ReadBWLT(path string) (exportID string, nonce []byte, ciphertext []byte, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to read .bwlt file: %w", err)
	}

	if len(data) < bwltMinSize {
		return "", nil, nil, fmt.Errorf("file too small to be a valid .bwlt file (%d bytes)", len(data))
	}

	// Verify magic bytes
	for i := 0; i < 4; i++ {
		if data[i] != bwltMagic[i] {
			return "", nil, nil, fmt.Errorf("not a .bwlt file (invalid magic bytes)")
		}
	}

	// Check version
	version := data[4]
	if version != bwltVersion {
		return "", nil, nil, fmt.Errorf("unsupported .bwlt version %d (expected %d)", version, bwltVersion)
	}

	// Extract export ID
	exportID = string(data[bwltHeaderSize : bwltHeaderSize+bwltExportIDLen])

	// Extract nonce
	nonceStart := bwltHeaderSize + bwltExportIDLen
	nonce = data[nonceStart : nonceStart+bwltNonceLen]

	// Extract ciphertext (rest of file)
	ciphertext = data[nonceStart+bwltNonceLen:]

	return exportID, nonce, ciphertext, nil
}
