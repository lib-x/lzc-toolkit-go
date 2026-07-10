package signature

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/archive"
	"github.com/lib-x/lpk-go/lpk"
)

// Verify verifies an LPK signature from src. It does not close src.
func Verify(ctx context.Context, src io.Reader, request VerifyRequest) (VerifyResult, error) {
	var result VerifyResult
	if err := contextError(ctx, "signature.verify"); err != nil {
		return result, err
	}
	if src == nil {
		return result, signatureError(lpkgo.CodeInvalidArgument, "signature.verify", fmt.Errorf("nil source"))
	}
	reader, err := lpk.Open(ctx, src, lpk.WithLimits(request.Limits))
	if err != nil {
		return result, err
	}
	defer reader.Close()
	return verifyReader(ctx, reader, request)
}

// VerifyFile verifies an LPK signature from a filesystem path.
func VerifyFile(ctx context.Context, filename string, request VerifyRequest) (VerifyResult, error) {
	var result VerifyResult
	if err := contextError(ctx, "signature.verify_file"); err != nil {
		return result, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return result, signatureError(lpkgo.CodeCommandFailed, "signature.verify_file", err)
	}
	defer file.Close()
	return Verify(ctx, file, request)
}

func verifyReader(ctx context.Context, reader *lpk.Reader, request VerifyRequest) (VerifyResult, error) {
	var result VerifyResult
	releaseBytes, err := readRequiredEntry(ctx, reader, releaseLockPath)
	if err != nil {
		return result, err
	}
	var lock releaseLock
	if err := json.Unmarshal(releaseBytes, &lock); err != nil {
		return result, integrityError("signature.verify", "invalid release.lock JSON")
	}
	result.AppID = lock.AppID
	result.Version = lock.Version
	result.ObjectCount = len(lock.Objects)
	if lock.Schema != releaseLockSchema {
		return result, integrityError("signature.verify", "invalid release.lock schema")
	}

	keyID := request.KeyID
	if keyID == "" {
		keyID, err = discoverKeyID(ctx, reader)
		if err != nil {
			return result, err
		}
	}
	result.KeyID = keyID
	sigBytes, err := readRequiredEntry(ctx, reader, "META/signatures/"+keyID+".sig")
	if err != nil {
		return result, err
	}
	var sig signatureDocument
	if err := json.Unmarshal(sigBytes, &sig); err != nil {
		return result, integrityError("signature.verify", "invalid signature JSON")
	}
	if sig.Schema != signatureSchema || sig.Algorithm != signatureAlg || sig.SignedFile != releaseLockPath || sig.KeyID != keyID {
		return result, integrityError("signature.verify", "invalid signature metadata")
	}
	signatureBytes, err := base64Decode(sig.Signature)
	if err != nil {
		return result, integrityError("signature.verify", "invalid signature encoding")
	}
	publicKey, err := publicKeyForVerify(ctx, reader, keyID, request.TrustedPublicKeyPEM)
	if err != nil {
		return result, err
	}
	if !ed25519.Verify(publicKey, releaseBytes, signatureBytes) {
		return result, integrityError("signature.verify", "signature mismatch")
	}
	if err := verifyObjects(ctx, reader, lock.Objects); err != nil {
		return result, err
	}
	result.Valid = true
	return result, nil
}

func publicKeyForVerify(ctx context.Context, reader *lpk.Reader, keyID string, trusted []byte) (ed25519.PublicKey, error) {
	if len(trusted) != 0 {
		return parsePublicKey(trusted)
	}
	keyBytes, err := readRequiredEntry(ctx, reader, "META/keys/"+keyID+".pub")
	if err != nil {
		return nil, err
	}
	publicKey, err := parsePublicKey(keyBytes)
	if err != nil {
		return nil, integrityError("signature.verify", "embedded public key invalid")
	}
	return publicKey, nil
}

func discoverKeyID(ctx context.Context, reader *lpk.Reader) (string, error) {
	entries, err := reader.Entries(ctx)
	if err != nil {
		return "", err
	}
	var ids []string
	for _, entry := range entries {
		if entry.Type != archive.EntryRegular || !strings.HasPrefix(entry.Name, "META/signatures/") || !strings.HasSuffix(entry.Name, ".sig") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Name, "META/signatures/"), ".sig")
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", integrityError("signature.verify", "signature file missing")
	}
	sort.Strings(ids)
	return ids[0], nil
}

func verifyObjects(ctx context.Context, reader *lpk.Reader, objects []releaseObject) error {
	entries, err := reader.Entries(ctx)
	if err != nil {
		return err
	}
	regulars := make(map[string]archive.Entry)
	for _, entry := range entries {
		if entry.Type == archive.EntryRegular && !strings.HasPrefix(entry.Name, "META/") {
			regulars[entry.Name] = entry
		}
	}
	seen := make(map[string]struct{}, len(objects))
	for _, object := range objects {
		if object.Path == "" || strings.HasPrefix(object.Path, "META/") {
			return invalidMetadata("invalid object path")
		}
		if _, exists := seen[object.Path]; exists {
			return invalidMetadata("duplicate object path")
		}
		seen[object.Path] = struct{}{}
		entry, ok := regulars[object.Path]
		if !ok {
			return integrityError("signature.verify", "listed object missing")
		}
		if entry.Size != object.Size {
			return integrityError("signature.verify", "object size mismatch")
		}
		digest, size, err := hashEntry(ctx, reader, object.Path)
		if err != nil {
			return err
		}
		if size != object.Size || object.Digest != "sha256:"+digest {
			return integrityError("signature.verify", "object digest mismatch")
		}
	}
	for name := range regulars {
		if _, ok := seen[name]; !ok {
			return integrityError("signature.verify", "unexpected object")
		}
	}
	return nil
}

func hashEntry(ctx context.Context, reader *lpk.Reader, name string) (string, int64, error) {
	contents, err := reader.OpenEntry(ctx, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", 0, integrityError("signature.verify", "entry missing")
		}
		return "", 0, err
	}
	defer contents.Close()
	hasher := sha256.New()
	counter := &countingWriter{}
	if _, err := io.Copy(io.MultiWriter(hasher, counter), contents); err != nil {
		return "", 0, signatureError(lpkgo.CodeIntegrityMismatch, "signature.verify", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), counter.written, nil
}

func readRequiredEntry(ctx context.Context, reader *lpk.Reader, name string) ([]byte, error) {
	contents, err := reader.OpenEntry(ctx, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, integrityError("signature.verify", "%s missing", name)
		}
		return nil, err
	}
	data, readErr := io.ReadAll(contents)
	closeErr := contents.Close()
	if readErr != nil {
		return nil, signatureError(lpkgo.CodeIntegrityMismatch, "signature.verify", readErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}
