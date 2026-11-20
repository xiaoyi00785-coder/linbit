package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

// Encrypted blob format (versioned, base64 fields joined by '.'):
//   v1.<salt_b64>.<nonce_b64>.<ciphertext_b64>
// Decryption needs the user's PIN plus the server-wide pepper; neither alone
// recovers the mnemonic. Scrypt is N=1<<15, r=8, p=1 — rfc7914 "interactive".
const (
	ksVersion   = "v1"
	ksSaltBytes = 16
	ksN         = 1 << 15
	ksR         = 8
	ksP         = 1
	ksKeyBytes  = 32
)

func deriveKey(pin, pepper string, salt []byte) (*[32]byte, error) {
	material := append([]byte(pin), 0)
	material = append(material, []byte(pepper)...)
	raw, err := scrypt.Key(material, salt, ksN, ksR, ksP, ksKeyBytes)
	if err != nil {
		return nil, err
	}
	var key [32]byte
	copy(key[:], raw)
	for i := range raw {
		raw[i] = 0
	}
	return &key, nil
}

// EncryptMnemonic seals the mnemonic under PIN+pepper. Panics only if the OS
// RNG fails, which is treated as unrecoverable.
func EncryptMnemonic(mnemonic, pin, pepper string) (string, error) {
	if mnemonic == "" {
		return "", errors.New("empty mnemonic")
	}
	if pin == "" {
		return "", errors.New("empty pin")
	}
	salt := make([]byte, ksSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	key, err := deriveKey(pin, pepper, salt)
	if err != nil {
		return "", err
	}
	defer zero(key[:])
	ct := secretbox.Seal(nil, []byte(mnemonic), &nonce, key)
	return fmt.Sprintf("%s.%s.%s.%s",
		ksVersion,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(nonce[:]),
		base64.RawStdEncoding.EncodeToString(ct),
	), nil
}

func DecryptMnemonic(blob, pin, pepper string) (string, error) {
	parts := splitN(blob, '.', 4)
	if len(parts) != 4 || parts[0] != ksVersion {
		return "", errors.New("malformed keystore blob")
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	nonceB, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	ct, err3 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || len(nonceB) != 24 {
		return "", errors.New("corrupt keystore blob")
	}
	var nonce [24]byte
	copy(nonce[:], nonceB)
	key, err := deriveKey(pin, pepper, salt)
	if err != nil {
		return "", err
	}
	defer zero(key[:])
	pt, ok := secretbox.Open(nil, ct, &nonce, key)
	if !ok {
		return "", ErrBadPIN
	}
	return string(pt), nil
}

var ErrBadPIN = errors.New("bad pin")

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func splitN(s string, sep byte, n int) []string {
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(s) && len(out) < n-1; i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
