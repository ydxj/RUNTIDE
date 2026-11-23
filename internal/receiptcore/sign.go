package receiptcore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Signing is optional and deliberately simple: an Ed25519 signature over the
// bytes of manifest.json (which in turn pins every file by SHA-256). A valid
// signature proves the manifest was signed by the holder of that private key;
// it proves nothing about who that is unless you already trust the key's
// fingerprint, which you obtain out of band. There is no PKI, no key
// registry and no revocation.

// SignatureInfo is the result of checking signature.json.
type SignatureInfo struct {
	Algorithm   string    `json:"algorithm"`
	Fingerprint string    `json:"fingerprint"` // first 16 hex chars of SHA-256(public key)
	PublicKey   string    `json:"publicKey"`
	SignedAt    time.Time `json:"signedAt"`
	Valid       bool      `json:"valid"`
	Trusted     bool      `json:"trusted"` // fingerprint is in the caller's trusted list
}

type signatureFile struct {
	Algorithm string    `json:"algorithm"`
	PublicKey string    `json:"publicKey"`
	Signature string    `json:"signature"`
	SignedAt  time.Time `json:"signedAt"`
}

// KeyPair is an Ed25519 signing key.
type KeyPair struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// Fingerprint identifies the public key.
func (k *KeyPair) Fingerprint() string { return Fingerprint(k.Public) }

// Fingerprint returns the short identifier of a public key.
func Fingerprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// GenerateKey creates a key pair and stores the private key (mode 0600) at path.
func GenerateKey(path string) (*KeyPair, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("%s already exists; refusing to overwrite a signing key", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(map[string]string{"algorithm": "ed25519", "privateKey": hex.EncodeToString(priv)})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return &KeyPair{Private: priv, Public: pub}, nil
}

// LoadKey reads a private key file written by GenerateKey.
func LoadKey(path string) (*KeyPair, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct{ Algorithm, PrivateKey string }
	if err := json.Unmarshal(b, &f); err != nil || f.Algorithm != "ed25519" {
		return nil, errors.New("not a RUNTIDE signing key file")
	}
	raw, err := hex.DecodeString(f.PrivateKey)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("signing key is corrupt")
	}
	priv := ed25519.PrivateKey(raw)
	return &KeyPair{Private: priv, Public: priv.Public().(ed25519.PublicKey)}, nil
}

// SignManifest returns the contents of signature.json for manifest bytes.
func (k *KeyPair) SignManifest(manifest []byte, at time.Time) ([]byte, error) {
	sum := sha256.Sum256(manifest)
	sig := ed25519.Sign(k.Private, sum[:])
	return json.MarshalIndent(signatureFile{Algorithm: "ed25519", PublicKey: hex.EncodeToString(k.Public), Signature: hex.EncodeToString(sig), SignedAt: at.UTC()}, "", "  ")
}

func checkSignature(sigJSON, manifest []byte, trusted []string) (*SignatureInfo, error) {
	var f signatureFile
	if err := json.Unmarshal(sigJSON, &f); err != nil {
		return nil, fmt.Errorf("signature.json is not valid JSON: %w", err)
	}
	if f.Algorithm != "ed25519" {
		return nil, fmt.Errorf("unsupported signature algorithm %q", f.Algorithm)
	}
	pub, err := hex.DecodeString(f.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("signature public key is malformed")
	}
	sig, err := hex.DecodeString(f.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("signature value is malformed")
	}
	sum := sha256.Sum256(manifest)
	info := &SignatureInfo{Algorithm: f.Algorithm, PublicKey: f.PublicKey, Fingerprint: Fingerprint(pub), SignedAt: f.SignedAt, Valid: ed25519.Verify(pub, sum[:], sig)}
	for _, t := range trusted {
		if t == info.Fingerprint {
			info.Trusted = true
		}
	}
	return info, nil
}
