package dualpane

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// exdevFS는 Rename만 EXDEV(크로스볼륨)로 실패하는 로컬 FS다.
// 내장→외장 이동(rename 불가 → 복사 후 삭제 폴백) 시뮬레이션용.
type exdevFS struct{ LocalFS }

func (exdevFS) Rename(o, n string) error {
	return &os.LinkError{Op: "rename", Old: o, New: n, Err: syscall.EXDEV}
}

// permFS는 Rename이 항상 권한 에러인 로컬 FS다 (폴백하면 안 되는 경우).
type permFS struct{ LocalFS }

func (permFS) Rename(o, n string) error {
	return &os.LinkError{Op: "rename", Old: o, New: n, Err: syscall.EACCES}
}

// TestMoveCrossVolumeFallback은 rename이 EXDEV면 복사 후 삭제로
// 이동이 완료되는지 확인한다 (내장→외장디스크 이동).
func TestMoveCrossVolumeFallback(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	fp := filepath.Join(srcDir, "move.bin")
	content := []byte("cross-volume-data")
	if err := os.WriteFile(fp, content, 0o644); err != nil {
		t.Fatal(err)
	}
	var progCalls int
	files, byts, err := moveSameFS(exdevFS{}, fp, exdevFS{}, dstDir, func(FileProg) { progCalls++ })
	if err != nil {
		t.Fatalf("폴백 이동 실패: %v", err)
	}
	if files != 1 || byts != int64(len(content)) {
		t.Errorf("결과=%d개 %dB, want 1개 %dB", files, byts, len(content))
	}
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Fatalf("원본이 남아 있음")
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "move.bin"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("목적지 내용 오류: %q %v", got, err)
	}
}

// TestMoveRenameErrorPassthrough는 EXDEV가 아닌 rename 실패가
// 그대로 에러로 나오는지(복사 시도 없음) 확인한다.
func TestMoveRenameErrorPassthrough(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	fp := filepath.Join(srcDir, "stay.bin")
	if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := moveSameFS(permFS{}, fp, permFS{}, dstDir, nil)
	if err == nil {
		t.Fatalf("에러 없음")
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("원인 바뀜: %v", err)
	}
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("원본이 사라짐: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "stay.bin")); !os.IsNotExist(err) {
		t.Fatalf("복사본이 생기면 안 됨")
	}
}
