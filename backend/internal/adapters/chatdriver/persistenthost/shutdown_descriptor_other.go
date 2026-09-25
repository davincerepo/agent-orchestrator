//go:build !windows

package persistenthost

func descriptorBusy(error) bool { return false }
