package dualpane

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

// Entry는 파일/폴더 한 항목이다.
type Entry struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// FS는 로컬/SFTP 공통 파일시스템 인터페이스다.
type FS interface {
	Name() string
	ListDir(path string) ([]Entry, error)
	Mkdir(path string) error
	Remove(path string) error // 파일 또는 빈 폴더
	RemoveAll(path string) error
	Rename(oldPath, newPath string) error
	OpenRead(path string) (io.ReadCloser, error)
	OpenWrite(path string, mode os.FileMode) (io.WriteCloser, error)
	Close() error
}

// LocalFS는 로컬 디스크 구현이다.
type LocalFS struct{}

func (LocalFS) Name() string { return "local" }

func (LocalFS) ListDir(path string) ([]Entry, error) {
	des, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, d := range des {
		full := filepath.Join(path, d.Name())
		if d.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(full); err == nil {
				out = append(out, Entry{d.Name(), full, st.Size(), st.ModTime(), st.IsDir()})
			}
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{d.Name(), full, info.Size(), info.ModTime(), d.IsDir()})
	}
	return out, nil
}

func (LocalFS) Mkdir(path string) error                     { return os.MkdirAll(path, 0o755) }
func (LocalFS) Remove(path string) error                    { return os.Remove(path) }
func (LocalFS) RemoveAll(path string) error                 { return os.RemoveAll(path) }
func (LocalFS) Rename(o, n string) error                    { return os.Rename(o, n) }
func (LocalFS) Close() error                                { return nil }
func (LocalFS) OpenRead(path string) (io.ReadCloser, error) { return os.Open(path) }
func (LocalFS) OpenWrite(path string, mode os.FileMode) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
}
