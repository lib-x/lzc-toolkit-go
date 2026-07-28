package media_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore/media"
)

func TestNormalizeProducesStoreReadyPNG(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 2000, 1299))
	for y := range 1299 {
		for x := range 2000 {
			input.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}

	asset, err := media.Normalize(context.Background(), bytes.NewReader(encoded.Bytes()), "channel.png")
	if err != nil {
		t.Fatal(err)
	}
	if asset.FileName != "channel.png" || asset.Width != 2000 || asset.Height != 1125 || asset.Format != "png" {
		t.Fatalf("asset=%#v", asset)
	}
	if len(asset.Data) < 8 || !bytes.Equal(asset.Data[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		t.Fatalf("output is not PNG: %x", asset.Data[:min(8, len(asset.Data))])
	}
}

func TestNormalizeFileKeepsScreenshotsInsideProjectRoot(t *testing.T) {
	root := t.TempDir()
	input := image.NewRGBA(image.Rect(0, 0, 640, 480))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "screen.png"), encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	asset, err := media.NormalizeFile(context.Background(), root, "screen.png")
	if err != nil || asset.Width != 640 || asset.Height != 360 {
		t.Fatalf("asset=%#v err=%v", asset, err)
	}
	if _, err := media.NormalizeFile(context.Background(), root, "../screen.png"); err == nil {
		t.Fatal("expected escaping path to fail")
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := media.NormalizeFile(context.Background(), root, "linked.png"); err == nil {
		t.Fatal("expected symbolic link to fail")
	}
}

func TestNormalizeRejectsUnsupportedOrUndersizedImages(t *testing.T) {
	if _, err := media.Normalize(context.Background(), bytes.NewReader([]byte("GIF89a")), "screen.gif"); err == nil {
		t.Fatal("expected unsupported image to fail")
	}

	input := image.NewRGBA(image.Rect(0, 0, 319, 319))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Normalize(context.Background(), bytes.NewReader(encoded.Bytes()), "small.png"); err == nil {
		t.Fatal("expected undersized image to fail")
	}
}

func TestNormalizeSanitizesUploadFileNameToBoundedASCII(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 640, 480))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("a", 150) + "-截图\r\n.png"
	asset, err := media.Normalize(context.Background(), bytes.NewReader(encoded.Bytes()), name)
	if err != nil {
		t.Fatal(err)
	}
	if asset.FileName != strings.Repeat("a", 120)+".png" {
		t.Fatalf("filename=%q", asset.FileName)
	}
}
