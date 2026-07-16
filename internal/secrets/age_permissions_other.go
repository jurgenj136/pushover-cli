//go:build !windows

package secrets

func protectAgePath(string) error { return nil }
