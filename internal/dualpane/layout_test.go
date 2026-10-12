package dualpane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestViewFixedHeight는 복사대기(상태바 2줄) 유무와 무관하게
// 화면 전체 줄 수가 터미널 높이와 정확히 일치하는지 확인한다.
func TestViewFixedHeight(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, h := range []int{24, 30, 40} {
		m := New(root, root)
		m.width, m.height = 100, h
		m = loadPanes(m)

		// 복사대기 없음
		m.yankFS = nil
		m.yankPaths = nil
		off := strings.Count(m.View(), "\n")

		// 복사대기 2개 (상태바에 목록 한 줄 추가 → 목록 영역이 한 줄 줄어 균형)
		m.yankFS = LocalFS{}
		m.yankPaths = []string{filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")}
		v := m.View()
		on := strings.Count(v, "\n")

		if off != on {
			t.Errorf("h=%d: 복사대기 유무에 따라 높이가 달라짐 (off=%d, on=%d)", h, off, on)
		}
		if on < h || on > h+1 {
			t.Errorf("h=%d: 화면 높이 이탈 (newlines=%d)", h, on)
		}
		if !strings.Contains(v, "복사 대기 2개") {
			t.Errorf("상태바에 복사 대기 목록 없음")
		}
	}
}
