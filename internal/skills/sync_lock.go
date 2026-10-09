package skills

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

func (p paths) lockStore(ctx context.Context) (*os.File, error) {
	if err := os.MkdirAll(p.store, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(p.store, ".sync.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		acquired, err := tryStoreLock(file)
		if err != nil {
			file.Close()
			return nil, err
		}
		if acquired {
			return file, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
