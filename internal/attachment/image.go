// Package attachment snapshots and validates local and remote image attachments.
package attachment

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const maxImagePixels uint64 = 40_000_000

// snapshotImage validates an already bounded byte snapshot. It deliberately
// derives media type and name from decoded bytes rather than an input filename
// or response header, which keeps external locations out of later output.
func snapshotImage(data []byte) (*domain.Attachment, error) {
	if len(data) == 0 {
		return nil, invalidImage()
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, invalidImage()
	}
	if uint64(config.Width) > maxImagePixels/uint64(config.Height) {
		return nil, apperror.New("attachment_dimensions", apperror.CategoryValidation, "attachment image dimensions are too large")
	}

	name, mediaType, ok := imageIdentity(format)
	if !ok || http.DetectContentType(data) != mediaType {
		return nil, invalidImage()
	}
	// DecodeConfig is the inexpensive policy check. A full decode then rejects
	// truncated payloads that carry a valid signature and header; it is reached
	// only after the bounded input and 40M-pixel ceiling above.
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return nil, invalidImage()
	}
	return &domain.Attachment{
		Name:      name,
		MediaType: mediaType,
		Bytes:     bytes.Clone(data),
	}, nil
}

func invalidImage() error {
	return apperror.New("attachment_image", apperror.CategoryValidation, "attachment must be a valid PNG, JPEG, or GIF image")
}

func imageIdentity(format string) (name, mediaType string, ok bool) {
	switch format {
	case "png":
		return "attachment.png", "image/png", true
	case "jpeg":
		return "attachment.jpg", "image/jpeg", true
	case "gif":
		return "attachment.gif", "image/gif", true
	default:
		return "", "", false
	}
}

func invalidScope() error {
	return apperror.New("attachment_scope", apperror.CategoryInternal, "attachment operation context is not initialized")
}

func wrapLocal(cause error) error {
	return apperror.Wrap("attachment_local", apperror.CategoryValidation, "attachment file cannot be read", cause)
}

func wrapRemote(code, message string, cause error, rawURL string) error {
	return apperror.Wrap(code, apperror.CategoryNetwork, message, cause, apperror.WithSecrets(rawURL))
}

func invalidRemoteURL() error {
	return apperror.New("attachment_url", apperror.CategoryValidation, "remote attachment URL must be an absolute HTTPS URL without credentials")
}

func attachmentTooLarge() error {
	return apperror.New("attachment_size", apperror.CategoryValidation, fmt.Sprintf("attachment exceeds %d bytes", domain.MaxAttachmentBytes))
}
