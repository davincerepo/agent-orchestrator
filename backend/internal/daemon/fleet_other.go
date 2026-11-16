//go:build !windows

package daemon

import (
	"context"
	"errors"
)

func RunFleet(_ context.Context, stopBackground bool, buildVersion string) error {
	if stopBackground {
		return errors.New("Fleet background shutdown currently requires Windows")
	}
	return Run(buildVersion)
}
