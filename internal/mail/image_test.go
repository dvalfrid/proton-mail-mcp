package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
)

// webp1x1 is a 1×1 lossless WebP.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	return img
}

func encoded(t *testing.T, format string, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "webp":
		var data []byte
		data, err = base64.StdEncoding.DecodeString(webp1x1)
		buf.Write(data)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeImageFormats(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		format, mime string
		w, h         int
	}{
		{"png", "image/png", 30, 20},
		{"gif", "image/png", 30, 20},
		{"jpeg", "image/jpeg", 30, 20},
		{"webp", "image/jpeg", 1, 1},
	} {
		img, err := DecodeImage(context.Background(), encoded(t, c.format, testImage(30, 20)))
		if err != nil {
			t.Errorf("%s: %v", c.format, err)
			continue
		}
		if img.Format != c.format || img.MIMEType != c.mime || img.Width != c.w || img.Height != c.h || img.Scaled {
			t.Errorf("%s: %+v", c.format, img)
		}
		// What is returned is a new encoding that decodes as what it says.
		cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil || "image/"+format != img.MIMEType || cfg.Width != c.w {
			t.Errorf("%s: returned data is %s %+v, %v", c.format, format, cfg, err)
		}
	}
}

func TestDecodeImageRefusesNonImages(t *testing.T) {
	t.Parallel()
	bmp := append([]byte("BM"), make([]byte, 60)...)
	for name, data := range map[string][]byte{
		"text":      []byte("this is not an image"),
		"empty":     nil,
		"bmp":       bmp,
		"cut png":   encoded(t, "png", testImage(30, 20))[:60],
		"png magic": []byte("\x89PNG\r\n\x1a\n"),
	} {
		if _, err := DecodeImage(context.Background(), data); !errors.Is(err, ErrNotAnImage) {
			t.Errorf("%s: err = %v, want ErrNotAnImage", name, err)
		}
	}
}

// pngHeader is a PNG that claims w×h pixels: enough for DecodeConfig, so
// the pixel limit is tested without making a huge image.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 0, 17)
	ihdr = append(ihdr, "IHDR"...)
	ihdr = binary.BigEndian.AppendUint32(ihdr, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 6, 0, 0, 0) // 8-bit RGBA
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, ihdr...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(ihdr))
}

// An image over the pixel limit is refused from its header, before it is
// decoded: a small file can claim a huge image.
func TestDecodeImageRefusesTooManyPixels(t *testing.T) {
	t.Parallel()
	_, err := DecodeImage(context.Background(), pngHeader(20_000, 20_000))
	if !errors.Is(err, ErrImageTooLarge) || !strings.Contains(err.Error(), "20000×20000") {
		t.Errorf("err = %v, want ErrImageTooLarge", err)
	}
}

func TestDecodeImageScalesDown(t *testing.T) {
	t.Parallel()
	img, err := DecodeImage(context.Background(), encoded(t, "png", testImage(3000, 100)))
	if err != nil {
		t.Fatal(err)
	}
	if !img.Scaled || img.Width != MaxImageEdge || img.Height != 100*MaxImageEdge/3000 ||
		img.OriginalWidth != 3000 || img.OriginalHeight != 100 || img.MIMEType != "image/png" {
		t.Errorf("image = %+v", img)
	}
}

// A PNG that is too large once encoded again (a photo saved as PNG) comes
// back as JPEG, with transparency drawn on white rather than black.
func TestDecodeImageLargePNGBecomesJPEG(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 1000, 1000))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range src.Pix {
		src.Pix[i] = uint8(r.Uint32())
	}
	for x := range 32 { // a transparent corner, larger than JPEG's blocks
		for y := range 32 {
			src.Set(x, y, color.NRGBA{})
		}
	}
	img, err := DecodeImage(context.Background(), encoded(t, "png", src))
	if err != nil {
		t.Fatal(err)
	}
	if img.MIMEType != "image/jpeg" || img.Format != "png" {
		t.Fatalf("image = %s from %s", img.MIMEType, img.Format)
	}
	out, err := jpeg.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := out.At(4, 4).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent pixel = %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}
}

const msgWithParts = "From: bob@example.org\r\nSubject: Parts\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=b1\r\n\r\n" +
	"--b1\r\nContent-Type: text/plain\r\n\r\nHello\r\n" +
	"--b1\r\nContent-Type: application/pdf; name=a.pdf\r\nContent-Disposition: attachment; filename=a.pdf\r\n\r\nPDFDATA\r\n" +
	"--b1\r\nContent-Type: image/png\r\nContent-ID: <x@example.org>\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
	"aW1hZ2UgYnl0ZXM=\r\n" + // "image bytes"
	"--b1--\r\n"

func TestExtractAttachment(t *testing.T) {
	t.Parallel()
	a, data, err := ExtractAttachment(strings.NewReader(msgWithParts), 2, 100)
	if err != nil || string(data) != "image bytes" || a.Index != 2 || !a.Inline || a.Type != "image/png" {
		t.Errorf("attachment 2 = %+v %q %v", a, data, err)
	}
	a, data, err = ExtractAttachment(strings.NewReader(msgWithParts), 1, 100)
	if err != nil || string(data) != "PDFDATA" || a.Name != "a.pdf" {
		t.Errorf("attachment 1 = %+v %q %v", a, data, err)
	}
	for _, index := range []int{0, 3, -1, MaxAttachments + 1} {
		if _, _, err := ExtractAttachment(strings.NewReader(msgWithParts), index, 100); !errors.Is(err, ErrAttachmentNotFound) {
			t.Errorf("index %d: err = %v", index, err)
		}
	}
	if _, _, err := ExtractAttachment(strings.NewReader(msgWithParts), 2, 5); !errors.Is(err, errAttachmentTooLarge) {
		t.Errorf("over the limit: err = %v", err)
	}

	// The indexes match what ParseMessage lists.
	m, err := ParseMessage(strings.NewReader(msgWithParts))
	if err != nil || len(m.Attachments) != 2 || m.Attachments[0].Index != 1 || m.Attachments[1].Index != 2 {
		t.Errorf("listed = %+v, %v", m.Attachments, err)
	}
}
