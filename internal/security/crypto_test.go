package security

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Keypair Generation ─────────────────────────────────────────────────────

func TestGenerateKeypair(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair() error: %v", err)
	}
	if len(kp.Public) != 32 {
		t.Errorf("public key len = %d, want 32", len(kp.Public))
	}
	if len(kp.Private) != 64 {
		t.Errorf("private key len = %d, want 64", len(kp.Private))
	}
}

func TestGenerateKeypair_Unique(t *testing.T) {
	kp1, _ := GenerateKeypair()
	kp2, _ := GenerateKeypair()

	if kp1.PublicKeyHex() == kp2.PublicKeyHex() {
		t.Error("two generated keypairs should have different public keys")
	}
}

func TestPublicKeyHex(t *testing.T) {
	kp, _ := GenerateKeypair()
	hex := kp.PublicKeyHex()

	if len(hex) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("hex len = %d, want 64", len(hex))
	}
}

// ─── Sign / Verify ──────────────────────────────────────────────────────────

func TestSignVerify(t *testing.T) {
	kp, _ := GenerateKeypair()
	message := []byte("hello TuTu network")

	sig := kp.Sign(message)
	if len(sig) != 64 { // Ed25519 signature is 64 bytes
		t.Errorf("signature len = %d, want 64", len(sig))
	}

	if !Verify(message, sig, kp.Public) {
		t.Error("Verify() should return true for valid signature")
	}
}

func TestVerify_WrongMessage(t *testing.T) {
	kp, _ := GenerateKeypair()
	sig := kp.Sign([]byte("original"))

	if Verify([]byte("tampered"), sig, kp.Public) {
		t.Error("Verify() should return false for wrong message")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	kp1, _ := GenerateKeypair()
	kp2, _ := GenerateKeypair()

	message := []byte("test message")
	sig := kp1.Sign(message)

	if Verify(message, sig, kp2.Public) {
		t.Error("Verify() should return false for wrong public key")
	}
}

func TestSignVerify_EmptyMessage(t *testing.T) {
	kp, _ := GenerateKeypair()
	sig := kp.Sign([]byte{})

	if !Verify([]byte{}, sig, kp.Public) {
		t.Error("Verify() should work for empty message")
	}
}

func TestSignVerify_LargeMessage(t *testing.T) {
	kp, _ := GenerateKeypair()
	message := make([]byte, 1<<20) // 1 MB
	for i := range message {
		message[i] = byte(i % 256)
	}

	sig := kp.Sign(message)
	if !Verify(message, sig, kp.Public) {
		t.Error("Verify() should work for large messages")
	}
}

// ─── Persistence ────────────────────────────────────────────────────────────

func TestLoadOrCreateKeypair_Creates(t *testing.T) {
	tmpHome := t.TempDir()
	kp, err := LoadOrCreateKeypair(tmpHome)
	if err != nil {
		t.Fatalf("LoadOrCreateKeypair() error: %v", err)
	}
	if kp == nil {
		t.Fatal("LoadOrCreateKeypair() returned nil")
	}

	// Check files were created
	keyDir := filepath.Join(tmpHome, "keys")
	if _, err := os.Stat(filepath.Join(keyDir, "node.pub")); os.IsNotExist(err) {
		t.Error("node.pub should exist")
	}
	if _, err := os.Stat(filepath.Join(keyDir, "node.key")); os.IsNotExist(err) {
		t.Error("node.key should exist")
	}
}

func TestLoadOrCreateKeypair_Loads(t *testing.T) {
	tmpHome := t.TempDir()

	// Create keypair
	kp1, _ := LoadOrCreateKeypair(tmpHome)

	// Load it again
	kp2, err := LoadOrCreateKeypair(tmpHome)
	if err != nil {
		t.Fatalf("LoadOrCreateKeypair() second call error: %v", err)
	}

	// Should be the same keypair
	if kp1.PublicKeyHex() != kp2.PublicKeyHex() {
		t.Error("loaded keypair should match created keypair")
	}
}

func TestLoadOrCreateKeypair_SignVerifyRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()

	kp, _ := LoadOrCreateKeypair(tmpHome)
	message := []byte("persistent identity test")
	sig := kp.Sign(message)

	// Reload and verify
	kp2, _ := LoadOrCreateKeypair(tmpHome)
	if !Verify(message, sig, kp2.Public) {
		t.Error("signature should verify after reloading keypair")
	}
}
