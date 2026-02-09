// Package security provides cryptographic identity and message signing.
// Architecture Part XI, Layer 2: Every node has an Ed25519 keypair.
// All messages between nodes are signed for authenticity.
package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Keypair holds the node's Ed25519 identity.
type Keypair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// GenerateKeypair creates a new Ed25519 keypair.
func GenerateKeypair() (*Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	return &Keypair{Public: pub, Private: priv}, nil
}

// LoadOrCreateKeypair loads an existing keypair from disk, or generates
// a new one on first run. Keys are stored in tutuHome/keys/.
func LoadOrCreateKeypair(tutuHome string) (*Keypair, error) {
	keyDir := filepath.Join(tutuHome, "keys")
	pubPath := filepath.Join(keyDir, "node.pub")
	privPath := filepath.Join(keyDir, "node.key")

	// Try loading existing keys
	pubBytes, pubErr := os.ReadFile(pubPath)
	privBytes, privErr := os.ReadFile(privPath)

	if pubErr == nil && privErr == nil {
		pub, err := hex.DecodeString(string(pubBytes))
		if err != nil {
			return nil, fmt.Errorf("decode public key: %w", err)
		}
		priv, err := hex.DecodeString(string(privBytes))
		if err != nil {
			return nil, fmt.Errorf("decode private key: %w", err)
		}
		return &Keypair{
			Public:  ed25519.PublicKey(pub),
			Private: ed25519.PrivateKey(priv),
		}, nil
	}

	// Generate new keypair
	kp, err := GenerateKeypair()
	if err != nil {
		return nil, err
	}

	// Save to disk
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(hex.EncodeToString(kp.Public)), 0644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}
	if err := os.WriteFile(privPath, []byte(hex.EncodeToString(kp.Private)), 0600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	return kp, nil
}

// PublicKeyHex returns the public key as a hex string (used as node ID).
func (kp *Keypair) PublicKeyHex() string {
	return hex.EncodeToString(kp.Public)
}

// Sign signs a message with the node's private key.
func (kp *Keypair) Sign(message []byte) []byte {
	return ed25519.Sign(kp.Private, message)
}

// Verify checks a signature against a public key.
func Verify(message, signature []byte, publicKey ed25519.PublicKey) bool {
	return ed25519.Verify(publicKey, message, signature)
}
