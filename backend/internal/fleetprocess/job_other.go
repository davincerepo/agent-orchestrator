//go:build !windows

package fleetprocess

func Contain(_ int) (func(), error) { return func() {}, nil }
