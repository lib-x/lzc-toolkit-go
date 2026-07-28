// Package media validates and normalizes screenshots for LazyCat official
// application information submissions.
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxBytes     = int64(15 << 20)
	MinDimension = 320
	MaxDimension = 3840
)

type Asset struct {
	Data     []byte
	FileName string
	Format   string
	Width    int
	Height   int
}

// NormalizeFile opens a project-relative regular file without following a
// symbolic-link component, then normalizes it for application submission.
func NormalizeFile(ctx context.Context, root, name string) (Asset, error) {
	root = strings.TrimSpace(root)
	name = strings.TrimSpace(name)
	if ctx == nil || root == "" || name == "" {
		return Asset{}, errors.New("normalize application screenshot file: context, root, and path are required")
	}
	if filepath.IsAbs(name) {
		return Asset{}, errors.New("normalize application screenshot file: path must be relative to project root")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Asset{}, fmt.Errorf("normalize application screenshot file: %w", err)
	}
	rootHandle, err := os.OpenRoot(absoluteRoot)
	if err != nil {
		return Asset{}, fmt.Errorf("normalize application screenshot file: %w", err)
	}
	target := filepath.Clean(filepath.Join(absoluteRoot, name))
	relative, err := filepath.Rel(absoluteRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = rootHandle.Close()
		return Asset{}, errors.New("normalize application screenshot file: path escapes project root")
	}
	current := "."
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		information, statErr := rootHandle.Lstat(current)
		if statErr != nil {
			_ = rootHandle.Close()
			return Asset{}, fmt.Errorf("normalize application screenshot file: %w", statErr)
		}
		if information.Mode()&os.ModeSymlink != 0 {
			_ = rootHandle.Close()
			return Asset{}, errors.New("normalize application screenshot file: symbolic links are not allowed")
		}
	}
	file, err := rootHandle.Open(relative)
	if err != nil {
		_ = rootHandle.Close()
		return Asset{}, fmt.Errorf("normalize application screenshot file: %w", err)
	}
	information, err := file.Stat()
	if err != nil {
		return Asset{}, errors.Join(fmt.Errorf("normalize application screenshot file: %w", err), file.Close(), rootHandle.Close())
	}
	if !information.Mode().IsRegular() {
		return Asset{}, errors.Join(errors.New("normalize application screenshot file: screenshot must be a regular file"), file.Close(), rootHandle.Close())
	}
	asset, normalizeErr := Normalize(ctx, file, filepath.Base(name))
	closeErr := errors.Join(file.Close(), rootHandle.Close())
	if normalizeErr != nil || closeErr != nil {
		return Asset{}, errors.Join(normalizeErr, closeErr)
	}
	return asset, nil
}

// Normalize validates a PNG or JPEG screenshot, center-crops it to the
// official store's 16:9 shape, and returns a deterministic PNG upload.
func Normalize(ctx context.Context, source io.Reader, fileName string) (Asset, error) {
	if ctx == nil || source == nil {
		return Asset{}, errors.New("normalize application screenshot: context and source are required")
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: source}, MaxBytes+1))
	if err != nil {
		return Asset{}, fmt.Errorf("normalize application screenshot: %w", err)
	}
	if int64(len(data)) > MaxBytes {
		return Asset{}, fmt.Errorf("normalize application screenshot: image exceeds %d bytes", MaxBytes)
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" && format != "jpeg" {
		return Asset{}, errors.New("normalize application screenshot: only PNG and JPEG images are supported")
	}
	if configuration.Width < MinDimension || configuration.Height < MinDimension || configuration.Width > MaxDimension || configuration.Height > MaxDimension {
		return Asset{}, fmt.Errorf("normalize application screenshot: dimensions must be between %d and %d pixels", MinDimension, MaxDimension)
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format {
		return Asset{}, errors.New("normalize application screenshot: image payload is invalid")
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	bounds := decoded.Bounds()
	crop := centerCrop16x9(bounds)
	output := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(output, output.Bounds(), decoded, crop.Min, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, output); err != nil {
		return Asset{}, fmt.Errorf("normalize application screenshot: encode PNG: %w", err)
	}
	if int64(encoded.Len()) > MaxBytes {
		return Asset{}, fmt.Errorf("normalize application screenshot: normalized image exceeds %d bytes", MaxBytes)
	}
	return Asset{
		Data: append([]byte(nil), encoded.Bytes()...), FileName: normalizedFileName(fileName),
		Format: "png", Width: crop.Dx(), Height: crop.Dy(),
	}, nil
}

func centerCrop16x9(bounds image.Rectangle) image.Rectangle {
	width, height := bounds.Dx(), bounds.Dy()
	cropWidth, cropHeight := width, height
	if width*9 > height*16 {
		cropWidth = height * 16 / 9
	} else if width*9 < height*16 {
		cropHeight = width * 9 / 16
	}
	left := bounds.Min.X + (width-cropWidth)/2
	top := bounds.Min.Y + (height-cropHeight)/2
	return image.Rect(left, top, left+cropWidth, top+cropHeight)
}

func normalizedFileName(value string) string {
	base := filepath.Base(strings.TrimSpace(value))
	extension := filepath.Ext(base)
	base = strings.TrimSuffix(base, extension)
	base = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			return character
		}
		return '-'
	}, base)
	base = strings.Trim(base, ".-_")
	if base == "" {
		base = "screenshot"
	}
	if len(base) > 120 {
		base = base[:120]
	}
	return base + ".png"
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}
