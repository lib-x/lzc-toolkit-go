package signature_test

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/signature"
)

func TestGenerateKeyPairWritesPKCS8AndSPKI(t *testing.T) {
	pair, err := signature.GenerateKeyPair(signature.GenerateKeyRequest{
		Directory: t.TempDir(),
		Name:      "upstream",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(pair.PrivateKeyPath, ".ed25519.private.pem") || !strings.HasSuffix(pair.PublicKeyPath, ".ed25519.public.pem") {
		t.Fatalf("paths = %#v", pair)
	}
	privateInfo, err := os.Stat(pair.PrivateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := privateInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("private mode = %v", got)
	}

	privatePEM, err := os.ReadFile(pair.PrivateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	privateBlock, _ := pem.Decode(privatePEM)
	if privateBlock == nil {
		t.Fatal("private PEM did not decode")
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(privateBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := privateKey.(ed25519.PrivateKey); !ok {
		t.Fatalf("private key type = %T", privateKey)
	}

	publicPEM, err := os.ReadFile(pair.PublicKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	publicBlock, _ := pem.Decode(publicPEM)
	if publicBlock == nil {
		t.Fatal("public PEM did not decode")
	}
	publicKey, err := x509.ParsePKIXPublicKey(publicBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := publicKey.(ed25519.PublicKey); !ok {
		t.Fatalf("public key type = %T", publicKey)
	}
}

func TestGenerateKeyPairRejectsExistingFilesUnlessForced(t *testing.T) {
	dir := t.TempDir()
	if _, err := signature.GenerateKeyPair(signature.GenerateKeyRequest{Directory: dir, Name: "dev"}); err != nil {
		t.Fatal(err)
	}
	if _, err := signature.GenerateKeyPair(signature.GenerateKeyRequest{Directory: dir, Name: "dev"}); !errors.Is(err, lpkgo.ErrConflict) {
		t.Fatalf("second GenerateKeyPair error = %v", err)
	}
	if _, err := signature.GenerateKeyPair(signature.GenerateKeyRequest{Directory: dir, Name: "dev", Force: true}); err != nil {
		t.Fatal(err)
	}
}
