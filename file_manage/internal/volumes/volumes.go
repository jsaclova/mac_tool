package volumes

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ListExternal은 외장디스크 마운트 지점 목록을 반환한다.
func ListExternal() []string {
	var cands []string
	switch runtime.GOOS {
	case "darwin":
		entries, err := os.ReadDir("/Volumes")
		if err != nil {
			return nil
		}
		for _, e := range entries {
			name := e.Name()
			// 시스템 볼륨/스냅샷 제외
			if strings.HasPrefix(name, "Macintosh HD") || strings.HasPrefix(name, "com.apple") {
				continue
			}
			p := filepath.Join("/Volumes", name)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				cands = append(cands, p)
			}
		}
	default:
		var bases []string
		if u := os.Getenv("USER"); u != "" {
			bases = append(bases, filepath.Join("/media", u), filepath.Join("/run/media", u))
		}
		bases = append(bases, "/mnt")
		for _, b := range bases {
			entries, err := os.ReadDir(b)
			if err != nil {
				continue
			}
			for _, e := range entries {
				p := filepath.Join(b, e.Name())
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					cands = append(cands, p)
				}
			}
		}
	}
	sort.Strings(cands)
	return cands
}
