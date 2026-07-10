package signature

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

// Sign signs an LPK read from src and writes the signed package to dst.
// It does not close src or dst.
func Sign(ctx context.Context, dst io.Writer, src io.Reader, request SignRequest) (SignResult, error) {
	result := SignResult{KeyID: request.KeyID}
	if err := contextError(ctx, "signature.sign"); err != nil {
		return result, err
	}
	if dst == nil || src == nil {
		return result, signatureError(lpkgo.CodeInvalidArgument, "signature.sign", fmt.Errorf("nil source or destination"))
	}
	keyID := request.KeyID
	if keyID == "" {
		keyID = "dev"
	}
	result.KeyID = keyID
	privateKey, err := parsePrivateKey(request.PrivateKeyPEM)
	if err != nil {
		return result, err
	}
	if len(request.PublicKeyPEM) == 0 {
		return result, signatureError(lpkgo.CodeInvalidArgument, "signature.sign", fmt.Errorf("missing public key"))
	}
	if _, err := parsePublicKey(request.PublicKeyPEM); err != nil {
		return result, err
	}

	reader, err := lpk.Open(ctx, src, lpk.WithLimits(request.Limits))
	if err != nil {
		return result, err
	}
	defer reader.Close()
	result.Layout = reader.Layout()
	result.Format = reader.Format()

	workDir, err := os.MkdirTemp("", "lzc-toolkit-sign-*")
	if err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign", err)
	}
	defer os.RemoveAll(workDir)
	if err := reader.Extract(ctx, workDir); err != nil {
		return result, err
	}
	if request.Resign {
		if err := os.RemoveAll(pathFromSlash(workDir, "META")); err != nil {
			return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign", err)
		}
	} else if hasSignatureData(workDir) {
		return result, signatureError(lpkgo.CodeConflict, "signature.sign", fmt.Errorf("package already signed"))
	}

	if err := writeSignatureMetadata(ctx, workDir, privateKey, request.PublicKeyPEM, keyID); err != nil {
		return result, err
	}
	writeResult, err := lpk.Write(ctx, dst, lpk.WriteRequest{Layout: result.Layout, Files: os.DirFS(workDir)})
	result.Size = writeResult.Size
	result.SHA256 = writeResult.SHA256
	return result, err
}

// SignFile signs srcFilename and atomically writes dstFilename.
// dstFilename may equal srcFilename.
func SignFile(ctx context.Context, dstFilename string, srcFilename string, request SignRequest) (SignResult, error) {
	var result SignResult
	if err := contextError(ctx, "signature.sign_file"); err != nil {
		return result, err
	}
	if dstFilename == "" || srcFilename == "" {
		return result, signatureError(lpkgo.CodeInvalidArgument, "signature.sign_file", fmt.Errorf("empty filename"))
	}
	source, err := os.Open(srcFilename)
	if err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	defer source.Close()

	directory := filepath.Dir(dstFilename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(dstFilename)+".tmp-*")
	if err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()

	result, err = Sign(ctx, temporary, source, request)
	if err != nil {
		return result, err
	}
	if err := temporary.Sync(); err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	if err := temporary.Close(); err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	if err := os.Chmod(temporaryName, 0o644); err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	if err := os.Rename(temporaryName, dstFilename); err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.sign_file", err)
	}
	committed = true
	return result, nil
}

func writeSignatureMetadata(ctx context.Context, workDir string, privateKey ed25519.PrivateKey, publicKeyPEM []byte, keyID string) error {
	if err := contextError(ctx, "signature.write_metadata"); err != nil {
		return err
	}
	objects, err := collectObjects(ctx, os.DirFS(workDir))
	if err != nil {
		return err
	}
	appID, version, err := packageIdentityFromDir(workDir)
	if err != nil {
		return err
	}
	lock := releaseLock{
		Schema:  releaseLockSchema,
		AppID:   appID,
		Version: version,
		Objects: objects,
	}
	releaseBytes, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_metadata", err)
	}
	signatureBytes := ed25519.Sign(privateKey, releaseBytes)
	signatureDoc, err := json.MarshalIndent(signatureDocument{
		Schema:     signatureSchema,
		Algorithm:  signatureAlg,
		KeyID:      keyID,
		SignedFile: releaseLockPath,
		Signature:  base64Encode(signatureBytes),
	}, "", "  ")
	if err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_metadata", err)
	}
	if err := os.MkdirAll(pathFromSlash(workDir, "META/keys"), 0o755); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_metadata", err)
	}
	if err := os.MkdirAll(pathFromSlash(workDir, "META/signatures"), 0o755); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_metadata", err)
	}
	files := map[string][]byte{
		releaseLockPath:                     releaseBytes,
		"META/keys/" + keyID + ".pub":       publicKeyPEM,
		"META/signatures/" + keyID + ".sig": signatureDoc,
	}
	for name, data := range files {
		if err := os.WriteFile(pathFromSlash(workDir, name), data, 0o644); err != nil {
			return signatureError(lpkgo.CodeCommandFailed, "signature.write_metadata", err)
		}
	}
	return nil
}
