package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // registers GIF for image.Decode
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP for image.Decode
)

// Limits on images. An image is written by its sender like the rest of the
// message, so its size is checked from the header before it is decoded.
const (
	MaxImageBytes  = 20 << 20   // an image attachment read at most
	MaxImagePixels = 50_000_000 // width × height decoded at most
	// MaxImageEdge is the longest side of an image returned. A larger image
	// is scaled down: Claude scales larger images down itself, so the
	// extra pixels only cost bytes.
	MaxImageEdge = 1568
	// maxPNGBytes is the largest re-encoded PNG returned; a larger one (a
	// photo saved as PNG) is returned as JPEG instead.
	maxPNGBytes = 1 << 20
	jpegQuality = 85
)

// imageFormats are the formats decoded, by the name image.DecodeConfig gives.
var imageFormats = map[string]bool{"png": true, "jpeg": true, "gif": true, "webp": true}

// ErrNotAnImage means an attachment's content is not a PNG, JPEG, GIF or
// WebP image, whatever its declared type says.
var ErrNotAnImage = errors.New("the attachment is not a PNG, JPEG, GIF or WebP image")

// ErrImageTooLarge means an image is over MaxImageBytes or MaxImagePixels.
var ErrImageTooLarge = errors.New("the image is too large to show")

// Image is an image attachment, decoded and encoded again: Data is what the
// decoder saw, never the sender's bytes passed through. What it shows is
// written by the sender: untrusted, like the message text.
type Image struct {
	Attachment                           // as read_message lists it
	Format                        string // the content's format: png, jpeg, gif or webp
	MIMEType                      string // of Data: image/png or image/jpeg
	Data                          []byte
	Width, Height                 int // of Data
	OriginalWidth, OriginalHeight int
	Scaled                        bool // Data is smaller than the original
}

// imageSlot lets one image be decoded at a time, so a few large images
// requested at once can't add up to more memory than one.
var imageSlot = make(chan struct{}, 1)

// Image returns attachment number index (Attachment.Index) of a message as
// an image. Like ReadMessage it never marks the message read, and it checks
// uidValidity the same way. Only the message's own MIME parts are read:
// nothing is fetched from the network.
func (c *Client) Image(ctx context.Context, folder string, uid, uidValidity uint32, index int) (*Image, error) {
	a, data, err := c.Attachment(ctx, folder, uid, uidValidity, index, MaxImageBytes)
	if errors.Is(err, errAttachmentTooLarge) {
		return nil, fmt.Errorf("%w: attachment %d is %d bytes, the limit is %d", ErrImageTooLarge, index, a.Size, MaxImageBytes)
	}
	if err != nil {
		return nil, err
	}
	img, err := DecodeImage(ctx, data)
	if err != nil {
		return nil, err
	}
	img.Attachment = a
	return img, nil
}

// DecodeImage checks that data is a PNG, JPEG, GIF or WebP image by its
// content, decodes it (the first frame of an animated GIF), scales it down
// to MaxImageEdge and encodes it again as PNG (from PNG or GIF) or JPEG.
func DecodeImage(ctx context.Context, data []byte) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !imageFormats[format] || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrNotAnImage
	}
	if px := int64(cfg.Width) * int64(cfg.Height); px > MaxImagePixels {
		return nil, fmt.Errorf("%w: %d×%d pixels, the limit is %d million", ErrImageTooLarge, cfg.Width, cfg.Height, MaxImagePixels/1_000_000)
	}

	select {
	case imageSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-imageSlot }()

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNotAnImage
	}
	b := src.Bounds()
	img := &Image{Format: format, OriginalWidth: b.Dx(), OriginalHeight: b.Dy()}
	out := src
	if w, h := b.Dx(), b.Dy(); max(w, h) > MaxImageEdge {
		nw, nh := max(w*MaxImageEdge/max(w, h), 1), max(h*MaxImageEdge/max(w, h), 1)
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		out, img.Scaled = dst, true
	}
	img.Width, img.Height = out.Bounds().Dx(), out.Bounds().Dy()

	var buf bytes.Buffer
	if format == "png" || format == "gif" {
		if err := png.Encode(&buf, out); err != nil {
			return nil, fmt.Errorf("encode image: %w", err)
		}
		if buf.Len() <= maxPNGBytes {
			img.MIMEType, img.Data = "image/png", buf.Bytes()
			return img, nil
		}
		buf.Reset()
	}
	if err := jpeg.Encode(&buf, onWhite(out), &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	img.MIMEType, img.Data = "image/jpeg", buf.Bytes()
	return img, nil
}

// onWhite draws an image with transparency on white, as a mail client shows
// it: JPEG has no transparency, and the encoder would make it black.
func onWhite(src image.Image) image.Image {
	if o, ok := src.(interface{ Opaque() bool }); ok && o.Opaque() {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}
