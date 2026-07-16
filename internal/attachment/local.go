package attachment

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

// Local snapshots a regular local file before validation or delivery. The
// supplied pathname is intentionally never put in a user-facing error.
func Local(ctx context.Context, path string) (*domain.Attachment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, apperror.Wrap("attachment_local", apperror.CategoryValidation, "attachment file cannot be read", err, apperror.WithSecrets(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, apperror.Wrap("attachment_local", apperror.CategoryValidation, "attachment file cannot be read", err, apperror.WithSecrets(path))
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("not a regular file")
		}
		return nil, apperror.Wrap("attachment_local", apperror.CategoryValidation, "attachment file must be a regular file", err, apperror.WithSecrets(path))
	}
	// Size is advisory: a concurrent writer can make it stale. The streaming
	// sentinel below is the only authoritative byte-limit decision.
	data, err := readBounded(ctx, f)
	if err != nil {
		if typed := attachmentError(err); typed != nil {
			return nil, typed
		}
		return nil, apperror.Wrap("attachment_local", apperror.CategoryValidation, "attachment file cannot be read", err, apperror.WithSecrets(path))
	}
	return snapshotImage(data)
}

func readBounded(ctx context.Context, reader io.Reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(domain.MaxAttachmentBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > domain.MaxAttachmentBytes {
		return nil, attachmentTooLarge()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
