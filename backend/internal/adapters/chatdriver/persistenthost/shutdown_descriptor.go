package persistenthost

import (
	"context"
	"errors"
	"time"
)

// Windows can briefly deny reads while a descriptor is being removed. Retry
// only sharing/lock violations; malformed records and unknown owners still
// fail closed. This also covers the first read before shutdown is requested.
func readShutdownDescriptor(ctx context.Context, dataDir, sessionID string) (Descriptor, error) {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	for {
		d, err := readDescriptor(dataDir, sessionID)
		if !descriptorBusy(err) {
			return d, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Descriptor{}, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}
