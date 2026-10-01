package media

import (
	"bytes"
	"context"
	"fmt"
	"github.com/chai2010/webp"
	"golang.org/x/image/draw"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"shopnext-laos/internal/domain"
	"strings"
)

const MaxUpload = 5 * 1024 * 1024

type Storage struct{ Dir string }

var filenameRE = regexp.MustCompile(`^[a-f0-9]{24}-[a-f0-9]{8}\.webp$`)

func (s *Storage) ValidateURL(url string) bool {
	if !strings.HasPrefix(url, "/media/") {
		return false
	}
	filename := strings.TrimPrefix(url, "/media/")
	if !filenameRE.MatchString(filename) {
		return false
	}
	info, err := os.Lstat(filepath.Join(s.Dir, filename))
	return err == nil && info.Mode().IsRegular()
}
func (s *Storage) ValidateContentURL(url string) bool {
	if !s.ValidateURL(url) {
		return false
	}
	f, err := os.Open(filepath.Join(s.Dir, strings.TrimPrefix(url, "/media/")))
	if err != nil {
		return false
	}
	defer f.Close()
	config, err := webp.DecodeConfig(f)
	return err == nil && config.Height > 0 && math.Abs(float64(config.Width)/float64(config.Height)-3) < 0.02
}
func (s *Storage) Save(ctx context.Context, reader io.Reader, mime string, content bool) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if mime != "image/webp" {
		return "", domain.Fail("WEBP_REQUIRED", 400)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaxUpload+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > MaxUpload {
		return "", domain.Fail("INVALID_UPLOAD_SIZE", 400)
	}
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
		return "", domain.Fail("INVALID_WEBP", 400)
	}
	config, err := webp.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40000000 {
		return "", domain.Fail("INVALID_IMAGE_DIMENSIONS", 400)
	}
	if content && math.Abs(float64(config.Width)/float64(config.Height)-3) > 0.02 {
		return "", domain.Fail("CONTENT_IMAGE_MUST_BE_3_TO_1", 400)
	}
	img, err := webp.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", domain.Fail("INVALID_WEBP", 400)
	}
	width, height := config.Width, config.Height
	if width > 1600 || height > 1600 {
		scale := 1600 / float64(max(width, height))
		width = max(1, int(float64(width)*scale))
		height = max(1, int(float64(height)*scale))
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		img = dst
	}
	var encoded bytes.Buffer
	if err := webp.Encode(&encoded, img, &webp.Options{Quality: 82}); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err := os.MkdirAll(s.Dir, 0750); err != nil {
		return "", err
	}
	filename := domain.Hash(encoded.String())[:24] + "-" + domain.Token(4) + ".webp"
	file, err := os.OpenFile(filepath.Join(s.Dir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		return "", err
	}
	if _, err := file.Write(encoded.Bytes()); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return "/media/" + filename, nil
}
func (s *Storage) Delete(ctx context.Context, url string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !strings.HasPrefix(url, "/media/") || !filenameRE.MatchString(strings.TrimPrefix(url, "/media/")) {
		return fmt.Errorf("invalid media path")
	}
	err := os.Remove(filepath.Join(s.Dir, strings.TrimPrefix(url, "/media/")))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
