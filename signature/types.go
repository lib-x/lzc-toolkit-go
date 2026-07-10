package signature

import (
	"github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

const (
	releaseLockSchema = "lazycat.lpk.release-lock/v1"
	signatureSchema   = "lazycat.lpk.signature/v1"
	signatureAlg      = "ed25519"
	releaseLockPath   = "META/release.lock"
)

// GenerateKeyRequest describes an Ed25519 key pair to create.
type GenerateKeyRequest struct {
	Directory string
	Name      string
	Force     bool
}

// KeyPair reports generated key file paths.
type KeyPair struct {
	PrivateKeyPath string
	PublicKeyPath  string
}

// SignRequest configures stream signing.
type SignRequest struct {
	PrivateKeyPEM []byte
	PublicKeyPEM  []byte
	KeyID         string
	Resign        bool
	Limits        archive.Limits
}

// SignResult reports the signed package container metadata.
type SignResult struct {
	Layout lpk.Layout
	Format archive.Format
	Size   int64
	SHA256 [32]byte
	KeyID  string
}

// VerifyRequest configures stream verification.
type VerifyRequest struct {
	TrustedPublicKeyPEM []byte
	KeyID               string
	Limits              archive.Limits
}

// VerifyResult reports signature verification details.
type VerifyResult struct {
	Valid       bool
	KeyID       string
	AppID       string
	Version     string
	ObjectCount int
}

type releaseLock struct {
	Schema  string          `json:"schema"`
	AppID   string          `json:"appid"`
	Version string          `json:"version"`
	Objects []releaseObject `json:"objects"`
}

type releaseObject struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type signatureDocument struct {
	Schema     string `json:"schema"`
	Algorithm  string `json:"algorithm"`
	KeyID      string `json:"key_id"`
	SignedFile string `json:"signed_file"`
	Signature  string `json:"signature"`
}
