package media

import (
	"bytes"
	"context"
	"github.com/chai2010/webp"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func TestWebPUpload(t *testing.T) {
	s := Storage{Dir: t.TempDir()}
	var buf bytes.Buffer
	if err := webp.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3000, 1000)), &webp.Options{Lossless: true}); err != nil {
		t.Fatal(err)
	}
	u, err := s.Save(context.Background(), bytes.NewReader(buf.Bytes()), "image/webp", true)
	if err != nil {
		t.Fatal(err)
	}
	if !s.ValidateURL(u) || !s.ValidateContentURL(u) {
		t.Fatal("saved image validation")
	}
	f, err := os.Open(filepath.Join(s.Dir, filepath.Base(u)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := webp.DecodeConfig(f)
	_ = f.Close()
	if err != nil || cfg.Width != 1600 || cfg.Height != 533 {
		t.Fatalf("resize %+v: %v", cfg, err)
	}
	if err := s.Delete(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if s.ValidateURL(u) {
		t.Fatal("file not deleted")
	}
	if s.ValidateURL("/media/../../etc/passwd") {
		t.Fatal("traversal")
	}
}
func TestUploadRejections(t *testing.T) {
	s := Storage{Dir: t.TempDir()}
	for _, test := range []struct{ raw, mime string }{{"bad", "image/webp"}, {"bad", "image/png"}, {"RIFFxxxxxxxxWEBP", "image/webp"}} {
		if _, err := s.Save(context.Background(), bytes.NewBufferString(test.raw), test.mime, false); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	var buf bytes.Buffer
	_ = webp.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 100, 100)), &webp.Options{Lossless: true})
	if _, err := s.Save(context.Background(), &buf, "image/webp", true); err == nil {
		t.Fatal("content ratio not enforced")
	}
	if _, err := s.Save(context.Background(), bytes.NewReader(make([]byte, MaxUpload+1)), "image/webp", false); err == nil {
		t.Fatal("size not enforced")
	}
}
