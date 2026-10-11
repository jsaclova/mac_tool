package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MoveToTrash는 경로들을 휴지통(~/.Trash)으로 이동한다. (복구 가능 삭제)
func MoveToTrash(paths []string) (moved int, firstErr error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	trash := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return 0, err
	}
	for _, p := range paths {
		base := filepath.Base(p)
		dest := filepath.Join(trash, base)
		if _, err := os.Lstat(dest); err == nil {
			dest = filepath.Join(trash, fmt.Sprintf("%s_%s", base, time.Now().Format("20060102-150405")))
		}
		if err := os.Rename(p, dest); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		moved++
	}
	return moved, firstErr
}
