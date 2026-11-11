package chainkit

// keystore.go：签名进程里的种子。v1 文件是 64 字节随机种子，用 scrypt 派生密钥后 AES-256-GCM 加密。
// 种子不出签名进程，对外只给 xpub 和地址。文件模式 0600，CreateKeystore 拒绝覆盖。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/scrypt"
)

type keystoreFile struct {
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	N          int    `json:"n"`
	R          int    `json:"r"`
	P          int    `json:"p"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	Testnet    bool   `json:"testnet"`
}

// Keystore 是已解锁的种子。String/GoString 不打印种子。
type Keystore struct {
	seed    []byte
	testnet bool
}

func (k *Keystore) String() string   { return "Keystore(locked-view)" }
func (k *Keystore) GoString() string { return k.String() }

// Testnet reports which network family the keystore was created for.
func (k *Keystore) Testnet() bool { return k.testnet }

// scrypt cost; tests may lower it.
var scryptN = 1 << 17

// CreateKeystore 生成新的随机种子，加密写到 path。模式 0600，已存在则拒绝覆盖。
func CreateKeystore(path string, passphrase []byte, testnet bool) (*Keystore, error) {
	if len(passphrase) < 12 {
		return nil, errors.New("keystore: passphrase must be at least 12 characters")
	}
	seed := make([]byte, 64)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	f := keystoreFile{Version: 1, KDF: "scrypt", N: scryptN, R: 8, P: 1, Testnet: testnet,
		Salt: make([]byte, 32), Nonce: make([]byte, 12)}
	if _, err := rand.Read(f.Salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(f.Nonce); err != nil {
		return nil, err
	}
	gcm, err := ksCipher(passphrase, f)
	if err != nil {
		return nil, err
	}
	f.Ciphertext = gcm.Seal(nil, f.Nonce, seed, ksAAD(f))
	b, _ := json.MarshalIndent(f, "", "  ")
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("keystore: %w", err)
	}
	defer fh.Close()
	if _, err := fh.Write(b); err != nil {
		return nil, err
	}
	return &Keystore{seed: seed, testnet: testnet}, nil
}

// OpenKeystore decrypts a keystore file.
func OpenKeystore(path string, passphrase []byte) (*Keystore, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keystore: %w", err)
	}
	var f keystoreFile
	if err := json.Unmarshal(b, &f); err != nil || f.Version != 1 || f.KDF != "scrypt" {
		return nil, errors.New("keystore: unsupported file")
	}
	gcm, err := ksCipher(passphrase, f)
	if err != nil {
		return nil, err
	}
	seed, err := gcm.Open(nil, f.Nonce, f.Ciphertext, ksAAD(f))
	if err != nil {
		return nil, errors.New("keystore: wrong passphrase or corrupted file")
	}
	return &Keystore{seed: seed, testnet: f.Testnet}, nil
}

func ksAAD(f keystoreFile) []byte {
	return []byte(fmt.Sprintf("linbit-keystore-v%d|%d|%d|%d|%v", f.Version, f.N, f.R, f.P, f.Testnet))
}

func ksCipher(pass []byte, f keystoreFile) (cipher.AEAD, error) {
	key, err := scrypt.Key(pass, f.Salt, f.N, f.R, f.P, 32)
	if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// 分支：充值地址在 branch 0，热钱包在 branch 1，储备（同一 HD 种子上的冷钱包替身）在 branch 2。
const (
	BranchDeposit uint32 = 0
	BranchHot     uint32 = 1
	BranchReserve uint32 = 2
)

// accountPath is the hardened BIP44/84 account path for a secp256k1 chain.
func accountPath(c Chain, testnet bool) ([]uint32, error) {
	h := HardenedOffset
	switch c {
	case ChainEVM:
		return []uint32{44 + h, 60 + h, 0 + h}, nil
	case ChainTron:
		return []uint32{44 + h, 195 + h, 0 + h}, nil
	case ChainBTC:
		coin := uint32(0)
		if testnet {
			coin = 1
		}
		return []uint32{84 + h, coin + h, 0 + h}, nil
	}
	return nil, fmt.Errorf("keystore: no secp256k1 path for %s", c)
}

// AccountXpub 导出 secp256k1 链的账户级 xpub。钱包用 <xpub>/0/i 派充值地址，热钱包是 <xpub>/1/0。
func (k *Keystore) AccountXpub(c Chain) (string, error) {
	acct, err := k.account(c)
	if err != nil {
		return "", err
	}
	return acct.Neuter().String(), nil
}

func (k *Keystore) account(c Chain) (*ExtKey, error) {
	m, err := NewMaster(k.seed, k.testnet)
	if err != nil {
		return nil, err
	}
	p, err := accountPath(c, k.testnet)
	if err != nil {
		return nil, err
	}
	return m.Derive(p...)
}

func (k *Keystore) secpKey(c Chain, branch, index uint32) (*ExtKey, error) {
	if index >= HardenedOffset || branch >= HardenedOffset {
		return nil, errors.New("keystore: index out of range")
	}
	acct, err := k.account(c)
	if err != nil {
		return nil, err
	}
	return acct.Derive(branch, index)
}

// solKey 用 SLIP-10 派生 ed25519（只能硬化）：
// m/44'/501'/(branch*2^20+index)'/0'。branch 0 充值，1 热钱包，2 储备。允许 0..BranchReserve。
func (k *Keystore) solKey(branch, index uint32) (ed25519.PrivateKey, error) {
	if index >= 1<<20 || branch > BranchReserve {
		return nil, errors.New("keystore: sol index out of range")
	}
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(k.seed)
	I := mac.Sum(nil)
	key, cc := I[:32], I[32:]
	for _, i := range []uint32{44, 501, branch<<20 | index, 0} {
		data := append([]byte{0}, key...)
		var ib [4]byte
		binary.BigEndian.PutUint32(ib[:], i|HardenedOffset)
		data = append(data, ib[:]...)
		m := hmac.New(sha512.New, cc)
		m.Write(data)
		I = m.Sum(nil)
		key, cc = I[:32], I[32:]
	}
	return ed25519.NewKeyFromSeed(key), nil
}

// SolAddresses 导出 n 个充值公钥地址和热钱包地址。
// Solana 不能从公钥派生子密钥，所以钱包拿地址列表，而不是 xpub。
func (k *Keystore) SolAddresses(n int) (deposit []string, hot string, err error) {
	for i := 0; i < n; i++ {
		pk, err := k.solKey(BranchDeposit, uint32(i))
		if err != nil {
			return nil, "", err
		}
		deposit = append(deposit, SolAddress(pk.Public().(ed25519.PublicKey)))
	}
	hk, err := k.solKey(BranchHot, 0)
	if err != nil {
		return nil, "", err
	}
	return deposit, SolAddress(hk.Public().(ed25519.PublicKey)), nil
}
