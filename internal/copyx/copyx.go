package copyx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Progress struct {
	Files int64
	Bytes int64
	Dest  string
}

// CopyInto는 src(파일/폴더)를 destDir/basename 밑에 재귀 복사한다.
func CopyInto(src, destDir string, onProgress func(Progress)) (files, bytes int64, err error) {
	info, err := os.Lstat(src)
	if err != nil {
		return 0, 0, err
	}
	dest := filepath.Join(destDir, filepath.Base(src))
	if info.IsDir() {
		err = filepath.WalkDir(src, func(path string, d os.DirEntry, werr error) error {
			if werr != nil {
				return nil // 접근 실패는 스킵
			}
			rel, _ := filepath.Rel(src, path)
			target := filepath.Join(dest, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			n, cerr := copyFile(path, target)
			if cerr != nil {
				return nil
			}
			files++
			bytes += n
			if onProgress != nil {
				onProgress(Progress{files, bytes, target})
			}
			return nil
		})
		return files, bytes, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, 0, fmt.Errorf("심볼릭 링크는 복사하지 않음: %s", src)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, 0, err
	}
	n, err := copyFile(src, dest)
	if err != nil {
		return 0, 0, err
	}
	return 1, n, nil
}

func copyFile(src, dest string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	fi, _ := in.Stat()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return 0, err
	}
	defer out.Close()
	n, err := io.Copy(out, in)
	return n, err
}
