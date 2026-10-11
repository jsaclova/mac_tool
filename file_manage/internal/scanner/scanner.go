package scanner

import (
	"container/heap"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
)

type FileEntry struct {
	Path string
	Size int64 // 실제 디스크 점유 바이트 (st_blocks*512)
}

type Result struct {
	TopFiles   []FileEntry
	DirSizes   []FileEntry
	TotalFiles int64
	TotalBytes int64
	Errors     int64
}

type inoKey struct {
	dev int32
	ino uint64
}

// minHeap은 상위 Top-N 유지용 최소 힙이다.
type minHeap []FileEntry

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool  { return h[i].Size < h[j].Size }
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(FileEntry)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// Event는 스캔 진행 상황이다. Done이 true면 Result가 최종값이다.
type Event struct {
	Files  int64
	Bytes  int64
	Errors int64
	Done   bool
	Result Result
}

// Scan은 워커풀 + 단일 집계기로 병렬 탐색한다.
// sameFS가 true면 시작 볼륨과 다른 파일시스템(외장디스크 등)은 내려가지 않는다.
func Scan(root string, topN int, minSize int64, excludes []string) Result {
	var res Result
	for ev := range ScanStream(root, topN, minSize, excludes) {
		if ev.Done {
			res = ev.Result
		}
	}
	return res
}

// ScanStream은 진행 이벤트를 흘려보내며 탐색한다. Done 이벤트 후 채널이 닫힌다.
func ScanStream(root string, topN int, minSize int64, excludes []string) <-chan Event {
	return scanStream(root, topN, minSize, excludes, true)
}

func scan(root string, topN int, minSize int64, excludes []string, sameFS bool) Result {
	return scanInner(root, topN, minSize, excludes, sameFS, nil)
}

func scanStream(root string, topN int, minSize int64, excludes []string, sameFS bool) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		scanInner(root, topN, minSize, excludes, sameFS, ch)
	}()
	return ch
}

func scanInner(root string, topN int, minSize int64, excludes []string, sameFS bool, prog chan<- Event) Result {
	root = filepath.Clean(root)
	workers := runtime.NumCPU()
	if workers < 4 {
		workers = 4
	}

	rootDev := deviceOf(root)
	excl := normalizeExcludes(root, excludes)

	type fstat struct {
		entry FileEntry
		dev   int32
		ino   uint64
		nlink uint16
	}
	fileCh := make(chan fstat, 4096)

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers*8)
	var totalFiles, totalBytes, errors atomic.Int64

	// 디렉토리 탐색: 세마포어 제한 고루틴 (데드락 없음)
	var walk func(dir string)
	walk = func(dir string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		entries, err := os.ReadDir(dir)
		if err != nil {
			errors.Add(1)
			return
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			if e.Type()&os.ModeSymlink != 0 {
				continue
			}
			if e.IsDir() {
				if isExcluded(full, excl) {
					continue
				}
				info, err := e.Info()
				if err != nil {
					errors.Add(1)
					continue
				}
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					if sameFS && rootDev != 0 && st.Dev != rootDev {
						continue // 다른 볼륨은 스킵
					}
				}
				wg.Add(1)
				go walk(full)
			} else {
				info, err := e.Info()
				if err != nil {
					errors.Add(1)
					continue
				}
				size, dev, ino, nlink := actualSize(info)
				fileCh <- fstat{FileEntry{full, size}, dev, ino, nlink}
			}
		}
	}

	// 집계기: 단일 고루틴이라 락 불필요
	var aggWg sync.WaitGroup
	dirSums := map[string]int64{}
	seenIno := map[inoKey]bool{}
	top := &minHeap{}
	heap.Init(top)
	aggWg.Add(1)
	go func() {
		defer aggWg.Done()
		var n int64
		for fs := range fileCh {
			f := fs.entry
			totalFiles.Add(1)
			if fs.nlink > 1 {
				k := inoKey{fs.dev, fs.ino}
				if seenIno[k] {
					continue // 같은 inode 중복 바이트 제외
				}
				seenIno[k] = true
			}
			totalBytes.Add(f.Size)
			tc := topChild(root, f.Path)
			dirSums[tc] += f.Size
			if f.Size >= minSize {
				if top.Len() < topN {
					heap.Push(top, f)
				} else if topN > 0 && f.Size > (*top)[0].Size {
					heap.Pop(top)
					heap.Push(top, f)
				}
			}
			n++
			if prog != nil && n%4096 == 0 {
				prog <- Event{Files: totalFiles.Load(), Bytes: totalBytes.Load(), Errors: errors.Load()}
			}
		}
	}()

	wg.Add(1)
	go walk(root)
	wg.Wait()
	close(fileCh)
	aggWg.Wait()

	all := make([]FileEntry, top.Len())
	for i := len(all) - 1; i >= 0; i-- {
		all[i] = heap.Pop(top).(FileEntry)
	}
	dirs := make([]FileEntry, 0, len(dirSums))
	for p, s := range dirSums {
		dirs = append(dirs, FileEntry{Path: p, Size: s})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Size > dirs[j].Size })

	res := Result{
		TopFiles:   all,
		DirSizes:   dirs,
		TotalFiles: totalFiles.Load(),
		TotalBytes: totalBytes.Load(),
		Errors:     errors.Load(),
	}
	if prog != nil {
		prog <- Event{Files: res.TotalFiles, Bytes: res.TotalBytes, Errors: res.Errors, Done: true, Result: res}
	}
	return res
}

func deviceOf(path string) int32 {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0
	}
	return st.Dev
}

func normalizeExcludes(root string, excludes []string) []string {
	out := make([]string, 0, len(excludes))
	for _, e := range excludes {
		if !filepath.IsAbs(e) {
			e = filepath.Join(root, e)
		}
		out = append(out, filepath.Clean(e))
	}
	return out
}

func isExcluded(path string, excl []string) bool {
	for _, e := range excl {
		if path == e || strings.HasPrefix(path, e+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func topChild(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return root
	}
	parts := strings.SplitN(rel, string(os.PathSeparator), 2)
	if len(parts) == 1 || parts[0] == "." {
		return path
	}
	return filepath.Join(root, parts[0])
}

// actualSize는 st_blocks 기반 실제 점유 바이트를 반환한다.
func actualSize(info os.FileInfo) (size int64, dev int32, ino uint64, nlink uint16) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if st.Blocks > 0 {
			size = st.Blocks * 512
		}
		return size, st.Dev, st.Ino, st.Nlink
	}
	return info.Size(), 0, 0, 1
}
