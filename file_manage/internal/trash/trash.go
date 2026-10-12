package trash

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// MoveToTrash는 경로들을 휴지통(~/.Trash)으로 이동한다. (복구 가능 삭제)
// 같은 볼륨이면 rename(즉시), 외장디스크 등 다른 볼륨이면
// rename이 안 되므로(EXDEV) 복사 후 원본을 삭제한다.
func MoveToTrash(paths []string) (moved int, firstErr error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	trash := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return 0, err
	}
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, p := range paths {
		base := filepath.Base(p)
		dest := filepath.Join(trash, base)
		if _, err := os.Lstat(dest); err == nil {
			dest = filepath.Join(trash, fmt.Sprintf("%s_%s", base, time.Now().Format("20060102-150405")))
		}
		if err := os.Rename(p, dest); err != nil {
			if !errors.Is(err, syscall.EXDEV) {
				fail(err)
				continue
			}
			if err := copyTree(p, dest); err != nil {
				fail(err)
				continue
			}
			if err := os.RemoveAll(p); err != nil {
				fail(err)
				continue
			}
		}
		moved++
	}
	return moved, firstErr
}

// copyTree는 파일/폴더를 재귀 복사한다 (크로스볼륨 휴지통용).
func copyTree(src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dest)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		des, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, d := range des {
			if err := copyTree(filepath.Join(src, d.Name()), filepath.Join(dest, d.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if err := out.Close(); err != nil && cerr == nil {
		cerr = err
	}
	return cerr
}
