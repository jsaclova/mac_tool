package dualpane

import (
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// join은 FS 종류에 맞는 경로 결합이다.
func join(fs FS, dir, name string) string {
	if _, ok := fs.(LocalFS); ok {
		return filepath.Join(dir, name)
	}
	return path.Join(dir, name)
}

// Copy는 src를 dstDir 밑에 재귀 복사한다. 경로는 FS별 구분자 사용.
func Copy(srcFS FS, srcPath string, dstFS FS, dstDir string, prog func(files, bytes int64)) (files, bytes int64, err error) {
	return copyInto(srcFS, srcPath, dstFS, dstDir, prog)
}

func copyInto(srcFS FS, src string, dstFS FS, dstDir string, prog func(files, bytes int64)) (files, bytes int64, err error) {
	base := filepath.Base(strings.TrimSuffix(src, "/"))
	if i := strings.LastIndex(src, "/"); i >= 0 && i < len(src)-1 {
		base = src[i+1:]
	}
	dest := join(dstFS, dstDir, base)
	var walk func(cur, rel string) error
	walk = func(cur, rel string) error {
		ents, err := srcFS.ListDir(cur)
		if err != nil {
			return err
		}
		if err := dstFS.Mkdir(join(dstFS, dest, rel)); err != nil {
			return err
		}
		for _, e := range ents {
			ep := join(srcFS, cur, e.Name)
			rp := join(dstFS, rel, e.Name)
			if e.IsDir {
				if err := walk(ep, rp); err != nil {
					return err
				}
				continue
			}
			n, err := copyFile(srcFS, ep, dstFS, join(dstFS, dest, rp))
			if err != nil {
				return fmt.Errorf("%s: %w", ep, err)
			}
			files++
			bytes += n
			if prog != nil {
				prog(files, bytes)
			}
		}
		return nil
	}
	// 단일 파일이면 직접 복사
	if isSingleFile(srcFS, src) {
		n, err := copyFile(srcFS, src, dstFS, dest)
		if err != nil {
			return 0, 0, err
		}
		return 1, n, nil
	}
	if err := walk(src, ""); err != nil {
		return files, bytes, err
	}
	return files, bytes, nil
}

func isSingleFile(fs FS, p string) bool {
	// 부모 목록에서 판별 (Stat이 인터페이스에 없어서)
	dir, base := split(fs, p)
	ents, err := fs.ListDir(dir)
	if err != nil {
		return true
	}
	for _, e := range ents {
		if e.Name == base {
			return !e.IsDir
		}
	}
	return true
}

func split(fs FS, p string) (dir, base string) {
	if _, ok := fs.(LocalFS); ok {
		return filepath.Split(p)
	}
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ".", p
	}
	return p[:i], p[i+1:]
}

func copyFile(srcFS FS, src string, dstFS FS, dest string) (int64, error) {
	in, err := srcFS.OpenRead(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := dstFS.OpenWrite(dest, 0o644)
	if err != nil {
		return 0, err
	}
	n, cerr := io.Copy(out, in)
	_ = out.Close()
	if cerr != nil {
		return n, cerr
	}
	return n, nil
}

// Move는 같은 FS면 rename, 다르면 복사 후 삭제한다.
func Move(srcFS FS, srcPath string, dstFS FS, dstDir string, prog func(files, bytes int64)) (files, bytes int64, err error) {
	if sameFS(srcFS, dstFS) {
		base := srcPath
		if i := strings.LastIndex(srcPath, "/"); i >= 0 {
			base = srcPath[i+1:]
		}
		if err := srcFS.Rename(srcPath, join(dstFS, dstDir, base)); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}
	files, bytes, err = Copy(srcFS, srcPath, dstFS, dstDir, prog)
	if err != nil {
		return files, bytes, err
	}
	if err := srcFS.RemoveAll(srcPath); err != nil {
		return files, bytes, err
	}
	return files, bytes, nil
}

func sameFS(a, b FS) bool {
	_, aLocal := a.(LocalFS)
	_, bLocal := b.(LocalFS)
	if aLocal && bLocal {
		return true
	}
	sa, aS := a.(*SftpFS)
	sb, bS := b.(*SftpFS)
	return aS && bS && sa == sb
}
