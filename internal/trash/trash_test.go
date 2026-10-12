package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// TestMoveToTrashNormal은 일반 삭제가 ~/.Trash로 이동하는지 확인한다.
func TestMoveToTrashNormal(t *testing.T) {
	home := tempHome(t)
	root := t.TempDir()
	fp := filepath.Join(root, "a.txt")
	if err := os.WriteFile(fp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := MoveToTrash([]string{fp})
	if err != nil || n != 1 {
		t.Fatalf("MoveToTrash=%d,%v", n, err)
	}
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Fatalf("원본이 남아 있음")
	}
	got, err := os.ReadFile(filepath.Join(home, ".Trash", "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("휴지통 내용 오류: %q %v", got, err)
	}
}

// TestMoveToTrashCollision은 동명 파일이 있을 때 시각 접미사로
// 겹치지 않게 버려지는지 확인한다.
func TestMoveToTrashCollision(t *testing.T) {
	home := tempHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".Trash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".Trash", "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	fp := filepath.Join(root, "a.txt")
	if err := os.WriteFile(fp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := MoveToTrash([]string{fp})
	if err != nil || n != 1 {
		t.Fatalf("MoveToTrash=%d,%v", n, err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".Trash", "a.txt"))
	if err != nil || string(got) != "old" {
		t.Fatalf("기존 휴지통 파일이 덮임: %q", got)
	}
}

// TestCopyTree는 크로스볼륨 폴백용 재귀 복사를 확인한다.
func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{
		{"a.txt", "A"},
		{"sub/b.txt", "B"},
	} {
		got, err := os.ReadFile(filepath.Join(dst, tc.path))
		if err != nil || string(got) != tc.want {
			t.Errorf("%s 내용 오류: %q %v", tc.path, got, err)
		}
	}
	if li, err := os.Lstat(filepath.Join(dst, "link")); err != nil || li.Mode()&os.ModeSymlink == 0 {
		t.Errorf("심볼릭링크 미복원: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "sub", "b.txt")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("권한 미보존: %v %v", fi, err)
	}
}

// TestMoveToTrashMissing은 없는 경로가 에러로 보고되는지 확인한다.
func TestMoveToTrashMissing(t *testing.T) {
	tempHome(t)
	n, err := MoveToTrash([]string{filepath.Join(t.TempDir(), "none")})
	if err == nil {
		t.Fatalf("에러 없음")
	}
	if n != 0 {
		t.Fatalf("moved=%d, want 0", n)
	}
	if !strings.Contains(err.Error(), "none") {
		t.Errorf("에러에 경로 없음: %v", err)
	}
}
