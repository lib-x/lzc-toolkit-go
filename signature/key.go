package signature

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

// GenerateKeyPair generates an Ed25519 private/public PEM pair.
func GenerateKeyPair(request GenerateKeyRequest) (KeyPair, error) {
	directory := request.Directory
	if directory == "" {
		directory = "."
	}
	if request.Name == "" {
		return KeyPair{}, signatureError(lpkgo.CodeInvalidArgument, "signature.generate_key_pair", fmt.Errorf("empty key name"))
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return KeyPair{}, signatureError(lpkgo.CodeCommandFailed, "signature.generate_key_pair", err)
	}

	privatePath := filepath.Join(directory, request.Name+".ed25519.private.pem")
	publicPath := filepath.Join(directory, request.Name+".ed25519.public.pem")
	if !request.Force {
		if _, err := os.Stat(privatePath); err == nil {
			return KeyPair{}, signatureError(lpkgo.CodeConflict, "signature.generate_key_pair", fmt.Errorf("private key already exists"))
		}
		if _, err := os.Stat(publicPath); err == nil {
			return KeyPair{}, signatureError(lpkgo.CodeConflict, "signature.generate_key_pair", fmt.Errorf("public key already exists"))
		}
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, signatureError(lpkgo.CodeCommandFailed, "signature.generate_key_pair", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return KeyPair{}, signatureError(lpkgo.CodeCommandFailed, "signature.generate_key_pair", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return KeyPair{}, signatureError(lpkgo.CodeCommandFailed, "signature.generate_key_pair", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	if err := writeFileAtomic(context.Background(), privatePath, privatePEM, 0o600); err != nil {
		return KeyPair{}, err
	}
	if err := writeFileAtomic(context.Background(), publicPath, publicPEM, 0o644); err != nil {
		return KeyPair{}, err
	}
	return KeyPair{PrivateKeyPath: privatePath, PublicKeyPath: publicPath}, nil
}

func parsePrivateKey(pemData []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, signatureError(lpkgo.CodeInvalidArgument, "signature.private_key", fmt.Errorf("invalid private key PEM"))
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, signatureError(lpkgo.CodeInvalidArgument, "signature.private_key", err)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, signatureError(lpkgo.CodeInvalidArgument, "signature.private_key", fmt.Errorf("private key is not Ed25519"))
	}
	return privateKey, nil
}

func parsePublicKey(pemData []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, signatureError(lpkgo.CodeInvalidArgument, "signature.public_key", fmt.Errorf("invalid public key PEM"))
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, signatureError(lpkgo.CodeInvalidArgument, "signature.public_key", err)
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, signatureError(lpkgo.CodeInvalidArgument, "signature.public_key", fmt.Errorf("public key is not Ed25519"))
	}
	return publicKey, nil
}
