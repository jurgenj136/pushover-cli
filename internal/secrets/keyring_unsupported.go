//go:build !linux && !windows && !darwin

package secrets

import (
	"context"
	"errors"
	"github.com/jurgenj136/pushover-cli/internal/config"
)

func openNative(context.Context, config.Backend) (Store, error) {
	return nil, unavailable(errors.New("native secret store is unsupported on this platform"))
}
