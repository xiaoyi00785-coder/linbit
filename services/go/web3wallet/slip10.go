package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
)

// slip10MasterEd25519 computes (key, chaincode) from a BIP39 seed per
// SLIP-0010 ed25519 variant: HMAC-SHA512(key="ed25519 seed", msg=seed).
func slip10MasterEd25519(seed []byte) (key, chainCode []byte) {
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(seed)
	sum := mac.Sum(nil)
	return sum[:32], sum[32:]
}

// slip10ChildEd25519 derives a hardened child. ed25519 does not support
// non-hardened derivation; callers must pass index >= 2^31.
func slip10ChildEd25519(key, chainCode []byte, index uint32) (childKey, childChain []byte) {
	var buf [37]byte
	buf[0] = 0x00
	copy(buf[1:33], key)
	binary.BigEndian.PutUint32(buf[33:], index)
	mac := hmac.New(sha512.New, chainCode)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	return sum[:32], sum[32:]
}

// ed25519NewKeyFromSeed is a thin named wrapper so the derive.go call site
// reads nicely.
func ed25519NewKeyFromSeed(seed []byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(seed)
}
