//go:build darwin && !cgo

package secrets

import (
	"context"
	"errors"

	"github.com/jurgenj136/pushover-cli/internal/config"
)

func openNative(context.Context, config.Backend) (Store, error) {
	return nil, unavailable(errors.New("native secrets require the supported macOS CGO binary; install an official release"))
}
