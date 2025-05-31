// GuiKeyStandaloneGo/pkg/crypto/keys.go
package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	// "io" // Not needed if using bytes.NewReader

	"github.com/google/uuid"
	// Use the specific libp2p crypto types for return values
	corecrypto "github.com/libp2p/go-libp2p/core/crypto"
)

func GenerateAppClientID() string {
	return uuid.NewString()
}

func GenerateLibp2pIdentitySeedHex() (string, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return "", fmt.Errorf("failed to generate libp2p identity seed: %w", err)
	}
	return hex.EncodeToString(seed), nil
}

// Libp2pKeyFromSeedHex now returns libp2p's core crypto types.
func Libp2pKeyFromSeedHex(seedHex string) (corecrypto.PrivKey, corecrypto.PubKey, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hex seed for libp2p key: %w", err)
	}
	if len(seed) != 32 {
		return nil, nil, fmt.Errorf("libp2p seed must be 32 bytes for Ed25519, got %d", len(seed))
	}

	// GenerateEd25519Key returns the libp2p core crypto types
	priv, pub, err := corecrypto.GenerateEd25519Key(bytes.NewReader(seed))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Ed25519 key from seed: %w", err)
	}
	return priv, pub, nil
}
