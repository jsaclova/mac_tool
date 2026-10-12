package dualpane

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// reportEvery는 대용량 파일 복사 중 진행 상황을 알리는 바이트 간격이다 (4MB).
const reportEvery = 4 << 20

// SizeOf는 단일 파일의 전체 바이트를 구한다 (진행률용). 모르면 -1.
func SizeOf(fs FS, p string) int64 {
	in, err := fs.OpenRead(p)
	if err != nil {
		return -1
	}
	defer in.Close()
	if st, ok := in.(interface{ Stat() (os.FileInfo, error) }); ok {
		if fi, err := st.Stat(); err == nil && !fi.IsDir() {
			return fi.Size()
		}
	}
	return -1
}

// join은 FS 종류에 맞는 경로 결합이다.
func join(fs FS, dir, name string) string {
	if _, ok := fs.(LocalFS); ok {
		return filepath.Join(dir, name)
	}
	return path.Join(dir, name)
}

// FileProg는 복사 진행 보고다 (항목 누적 + 현재 파일별).
type FileProg struct {
	Files     int64  // 현재 항목에서 완료한 파일 수
	Bytes     int64  // 현재 항목에서 처리한 바이트 (누적)
	Name      string // 현재 파일 이름
	FileTotal int64  // 현재 파일 전체 바이트 (-1 모름)
	FileDone  int64  // 현재 파일 처리 바이트
}

// Copy는 src를 dstDir 밑에 재귀 복사한다. 경로는 FS별 구분자 사용.
func Copy(srcFS FS, srcPath string, dstFS FS, dstDir string, prog func(FileProg)) (files, bytes int64, err error) {
	return copyInto(srcFS, srcPath, dstFS, dstDir, prog)
}

func copyInto(srcFS FS, src string, dstFS FS, dstDir string, prog func(FileProg)) (files, bytes int64, err error) {
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
			var cur, last int64
			ft := int64(-1)
			if e.Size > 0 {
				ft = e.Size
			}
			n, err := copyFile(srcFS, ep, dstFS, join(dstFS, dest, rp), func(d int64) {
				cur += d
				if prog != nil && cur-last >= reportEvery {
					last = cur
					prog(FileProg{files, bytes + cur, e.Name, ft, cur})
				}
			})
			if err != nil {
				return fmt.Errorf("%s: %w", ep, err)
			}
			files++
			bytes += n
			if prog != nil {
				prog(FileProg{files, bytes, e.Name, ft, n})
			}
		}
		return nil
	}
	// 단일 파일이면 직접 복사 (4MB마다 진행 보고, 전체 크기도 함께)
	if isSingleFile(srcFS, src) {
		total := SizeOf(srcFS, src)
		base := src
		if i := lastSep(src); i >= 0 && i < len(src)-1 {
			base = src[i+1:]
		}
		var cur, last int64
		n, err := copyFile(srcFS, src, dstFS, dest, func(d int64) {
			cur += d
			if prog != nil && cur-last >= reportEvery {
				last = cur
				prog(FileProg{0, cur, base, total, cur})
			}
		})
		if err != nil {
			return 0, 0, err
		}
		if prog != nil {
			prog(FileProg{1, n, base, total, n})
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

func copyFile(srcFS FS, src string, dstFS FS, dest string, chunk func(n int64)) (int64, error) {
	in, err := srcFS.OpenRead(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := dstFS.OpenWrite(dest, 0o644)
	if err != nil {
		return 0, err
	}
	var n int64
	buf := make([]byte, 1<<20) // 1MB씩 읽어 시스템콜을 줄인다
	for {
		nr, er := in.Read(buf)
		if nr > 0 {
			nw, ew := out.Write(buf[:nr])
			n += int64(nw)
			if chunk != nil {
				chunk(int64(nw))
			}
			if ew != nil {
				_ = out.Close()
				return n, ew
			}
			if nw != nr {
				_ = out.Close()
				return n, io.ErrShortWrite
			}
		}
		if er != nil {
			_ = out.Close()
			if er == io.EOF {
				return n, nil
			}
			return n, er
		}
	}
}

// Move는 같은 FS면 rename, 다르면 복사 후 삭제한다.
// 같은 로컬FS라도 볼륨이 다르면(내장→외장) rename이 EXDEV로 실패하므로
// 그때도 복사 후 삭제로 폴백한다.
func Move(srcFS FS, srcPath string, dstFS FS, dstDir string, prog func(FileProg)) (files, bytes int64, err error) {
	if sameFS(srcFS, dstFS) {
		return moveSameFS(srcFS, srcPath, dstFS, dstDir, prog)
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

// moveSameFS는 같은 FS 안에서 rename한다.
// 볼륨이 다르면 rename이 EXDEV로 실패하므로 복사 후 삭제로 폴백한다.
func moveSameFS(srcFS FS, srcPath string, dstFS FS, dstDir string, prog func(FileProg)) (files, bytes int64, err error) {
	base := srcPath
	if i := strings.LastIndex(srcPath, "/"); i >= 0 {
		base = srcPath[i+1:]
	}
	if rerr := srcFS.Rename(srcPath, join(dstFS, dstDir, base)); rerr == nil {
		return 0, 0, nil
	} else if !errors.Is(rerr, syscall.EXDEV) {
		return 0, 0, rerr
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
