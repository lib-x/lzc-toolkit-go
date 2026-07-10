package signature_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/signature"
)

func TestVerifySignedPackage(t *testing.T) {
	keys := generateKeys(t)
	source := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	signed := signPackage(t, source, keys, signature.SignRequest{KeyID: "dev"})

	result, err := signature.Verify(context.Background(), bytes.NewReader(signed), signature.VerifyRequest{KeyID: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.KeyID != "dev" || result.AppID != "cloud.lazycat.apps.demo" || result.Version != "2.0.0" || result.ObjectCount == 0 {
		t.Fatalf("Verify result = %#v", result)
	}
	if _, err := signature.Verify(context.Background(), bytes.NewReader(signed), signature.VerifyRequest{
		KeyID:               "dev",
		TrustedPublicKeyPEM: keys.publicPEM,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyTamperCases(t *testing.T) {
	keys := generateKeys(t)
	source := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	signed := signPackage(t, source, keys, signature.SignRequest{KeyID: "dev"})

	tests := []struct {
		name   string
		mutate func(string)
	}{
		{
			name: "listed file content",
			mutate: func(root string) {
				if err := os.WriteFile(slashPath(root, "payload.txt"), []byte("changed"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "listed file size",
			mutate: func(root string) {
				if err := os.WriteFile(slashPath(root, "payload.txt"), []byte("changed-size"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "release lock object digest",
			mutate: func(root string) {
				path := slashPath(root, "META/release.lock")
				lock := readJSONFile[map[string]any](t, path)
				objects := lock["objects"].([]any)
				first := objects[0].(map[string]any)
				first["digest"] = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
				writeJSONFile(t, path, lock)
			},
		},
		{
			name: "signature base64",
			mutate: func(root string) {
				path := slashPath(root, "META/signatures/dev.sig")
				sig := readJSONFile[map[string]any](t, path)
				sig["signature"] = "not-base64"
				writeJSONFile(t, path, sig)
			},
		},
		{
			name: "embedded public key",
			mutate: func(root string) {
				if err := os.WriteFile(slashPath(root, "META/keys/dev.pub"), []byte("bad key"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "key id",
			mutate: func(root string) {
				path := slashPath(root, "META/signatures/dev.sig")
				sig := readJSONFile[map[string]any](t, path)
				sig["key_id"] = "other"
				data, err := json.MarshalIndent(sig, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "signed file",
			mutate: func(root string) {
				path := slashPath(root, "META/signatures/dev.sig")
				sig := readJSONFile[map[string]any](t, path)
				sig["signed_file"] = "manifest.yml"
				writeJSONFile(t, path, sig)
			},
		},
		{
			name: "unexpected file",
			mutate: func(root string) {
				if err := os.WriteFile(slashPath(root, "extra.txt"), []byte("extra"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "remove listed file",
			mutate: func(root string) {
				if err := os.Remove(slashPath(root, "payload.txt")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tampered := rewritePackage(t, signed, test.mutate)
			result, err := signature.Verify(context.Background(), bytes.NewReader(tampered), signature.VerifyRequest{KeyID: "dev"})
			if !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
				t.Fatalf("Verify error = %v, result = %#v", err, result)
			}
			if result.Valid {
				t.Fatalf("Verify result = %#v; want invalid", result)
			}
		})
	}
}
