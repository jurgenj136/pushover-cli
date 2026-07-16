package attachment

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestSnapshotImageSupportedFormats(t *testing.T) {
	for name, encode := range map[string]func(*bytes.Buffer) error{
		"png":  func(buffer *bytes.Buffer) error { return png.Encode(buffer, testImage()) },
		"jpeg": func(buffer *bytes.Buffer) error { return jpeg.Encode(buffer, testImage(), nil) },
		"gif":  func(buffer *bytes.Buffer) error { return gif.Encode(buffer, testImage(), nil) },
	} {
		t.Run(name, func(t *testing.T) {
			var data bytes.Buffer
			if err := encode(&data); err != nil {
				t.Fatal(err)
			}
			attachment, err := snapshotImage(data.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if attachment.MediaType != "image/"+map[string]string{"png": "png", "jpeg": "jpeg", "gif": "gif"}[name] {
				t.Fatalf("media type = %q", attachment.MediaType)
			}
			data.Bytes()[0] ^= 0xff
			if attachment.Bytes[0] == data.Bytes()[0] {
				t.Fatal("snapshot retained caller buffer")
			}
		})
	}
}

func TestSnapshotImageRejectsCorruptAndOversizedDimensions(t *testing.T) {
	if _, err := snapshotImage([]byte("not an image")); err == nil {
		t.Fatal("corrupt image accepted")
	}
	truncated := pngWithDimensions(t, 1, 1)[:33] // valid signature/IHDR, no image data.
	if _, err := snapshotImage(truncated); err == nil {
		t.Fatal("truncated PNG with a recognizable header accepted")
	}
	data := pngWithDimensions(t, 8_000, 8_000)
	if _, err := snapshotImage(data); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}

func testImage() image.Image {
	image := image.NewRGBA(image.Rect(0, 0, 1, 1))
	image.Set(0, 0, color.RGBA{R: 1, A: 255})
	return image
}
