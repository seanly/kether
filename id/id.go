package id

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	hrp             = "keth"
	versionByte     = 0x00
	DomainRecord    = "kether/record/v2"
	DomainGrant     = "kether/grant/v1"
	DomainHello     = "kether/hello/v1"
	DomainChallenge = "kether/challenge/v1"
	secretLen       = btcec.PrivKeyBytesLen
	keyFileLen      = secretLen + 16
)

// PrivateKey is the agent's secp256k1 key and the UUID written into its record.
// The secret never appears in logs from this package.
type PrivateKey struct {
	k    *btcec.PrivateKey
	uuid [16]byte
}

// Public is the corresponding public key.
type Public struct {
	k *btcec.PublicKey
}

func Generate() (*PrivateKey, error) {
	k, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, err
	}
	u, err := newUUID()
	if err != nil {
		return nil, err
	}
	return &PrivateKey{k: evenY(k), uuid: u}, nil
}

// UUID is the canonical 8-4-4-4-12 identifier stored with this key.
func (k *PrivateKey) UUID() string {
	return formatUUID(k.uuid)
}

// evenY negates the secret when the public Y coordinate is odd so the
// libp2p peer ID matches the BIP340 x-only key.
func evenY(k *btcec.PrivateKey) *btcec.PrivateKey {
	if k.PubKey().SerializeCompressed()[0] == 0x02 {
		return k
	}
	s := new(btcec.ModNScalar)
	s.Set(&k.Key)
	s.Negate()
	return btcec.PrivKeyFromScalar(s)
}

func (k *PrivateKey) Public() *Public {
	return &Public{k: k.k.PubKey()}
}

func (k *PrivateKey) Libp2p() (crypto.PrivKey, error) {
	return crypto.UnmarshalSecp256k1PrivateKey(k.k.Serialize())
}

// Save writes the 32-byte secret and 16-byte UUID with mode 0600.
func (k *PrivateKey) Save(path string) error {
	buf := make([]byte, keyFileLen)
	copy(buf, k.k.Serialize())
	copy(buf[secretLen:], k.uuid[:])
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Load reads a private key and rejects files other users can access.
// A 32-byte file from an older build gains a UUID and is rewritten in place.
func Load(path string) (*PrivateKey, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("id: private key file is too permissive")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch len(b) {
	case secretLen:
		u, err := newUUID()
		if err != nil {
			return nil, err
		}
		k, _ := btcec.PrivKeyFromBytes(b)
		key := &PrivateKey{k: evenY(k), uuid: u}
		if err := key.Save(path); err != nil {
			return nil, err
		}
		return key, nil
	case keyFileLen:
		k, _ := btcec.PrivKeyFromBytes(b[:secretLen])
		var u [16]byte
		copy(u[:], b[secretLen:])
		if _, err := ParseUUID(formatUUID(u)); err != nil {
			return nil, errors.New("id: invalid uuid")
		}
		return &PrivateKey{k: evenY(k), uuid: u}, nil
	default:
		return nil, errors.New("id: invalid private key length")
	}
}

func newUUID() ([16]byte, error) {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return u, err
	}
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return u, nil
}

func formatUUID(u [16]byte) string {
	h := hex.EncodeToString(u[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ParseUUID accepts the canonical lowercase 8-4-4-4-12 form.
func ParseUUID(s string) ([16]byte, error) {
	var out [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return out, errors.New("id: uuid")
	}
	raw := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	if strings.ToLower(raw) != raw {
		return out, errors.New("id: uuid")
	}
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 16 {
		return out, errors.New("id: uuid")
	}
	if b[6]&0xf0 != 0x40 || b[8]&0xc0 != 0x80 {
		return out, errors.New("id: uuid")
	}
	copy(out[:], b)
	return out, nil
}

func (p *Public) XOnly() []byte {
	return schnorr.SerializePubKey(p.k)
}

func (p *Public) AgentID() (string, error) {
	raw := make([]byte, 0, 33)
	raw = append(raw, versionByte)
	raw = append(raw, p.XOnly()...)
	five, err := bech32.ConvertBits(raw, 8, 5, true)
	if err != nil {
		return "", err
	}
	return bech32.EncodeM(hrp, five)
}

func (p *Public) PeerID() (peer.ID, error) {
	pk, err := crypto.UnmarshalSecp256k1PublicKey(p.k.SerializeCompressed())
	if err != nil {
		return "", err
	}
	return peer.IDFromPublicKey(pk)
}

func ParseXOnly(b []byte) (*Public, error) {
	pk, err := schnorr.ParsePubKey(b)
	if err != nil {
		return nil, err
	}
	return &Public{k: pk}, nil
}

// ParseCompressed reads a 33-byte compressed secp256k1 public key.
func ParseCompressed(b []byte) (*Public, error) {
	pk, err := btcec.ParsePubKey(b)
	if err != nil {
		return nil, err
	}
	return &Public{k: pk}, nil
}

func ParseAgentID(s string) (*Public, error) {
	got, data, ver, err := bech32.DecodeGeneric(s)
	if err != nil {
		return nil, err
	}
	if got != hrp || ver != bech32.VersionM {
		return nil, errors.New("id: not a keth bech32m id")
	}
	raw, err := bech32.ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, err
	}
	if len(raw) != 33 || raw[0] != versionByte {
		return nil, errors.New("id: unsupported agent id version")
	}
	return ParseXOnly(raw[1:])
}

// Sign hashes domain || 0x00 || msg with SHA-256 and signs that digest with BIP340.
func Sign(k *PrivateKey, domain string, msg []byte) ([]byte, error) {
	sum := digest(domain, msg)
	sig, err := schnorr.Sign(k.k, sum)
	if err != nil {
		return nil, err
	}
	return sig.Serialize(), nil
}

func Verify(p *Public, domain string, msg, sig []byte) bool {
	if p == nil || len(sig) != 64 {
		return false
	}
	parsed, err := schnorr.ParseSignature(sig)
	if err != nil {
		return false
	}
	return parsed.Verify(digest(domain, msg), p.k)
}

func digest(domain string, msg []byte) []byte {
	h := sha256.New()
	_, _ = h.Write([]byte(domain))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(msg)
	return h.Sum(nil)
}

func MustAgentID(p *Public) string {
	s, err := p.AgentID()
	if err != nil {
		panic(err)
	}
	return s
}

func DecodeVersion(s string) (byte, error) {
	_, data, ver, err := bech32.DecodeGeneric(s)
	if err != nil {
		return 0, err
	}
	if ver != bech32.VersionM {
		return 0, fmt.Errorf("id: checksum version %d", ver)
	}
	raw, err := bech32.ConvertBits(data, 5, 8, false)
	if err != nil {
		return 0, err
	}
	if len(raw) < 1 {
		return 0, errors.New("id: empty")
	}
	return raw[0], nil
}
