package dualpane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key는 글자 키 입력 메시지를 만든다.
func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// runOpToCompletion은 runOp가 돌려준 cmd를 done opMsg가 나올 때까지 소진한다.
func runOpToCompletion(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	sawProgress := false
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		if om, ok := msg.(opMsg); ok {
			if !om.done {
				sawProgress = true
			}
			next, _ := m.Update(om)
			m = next.(Model)
			if om.done {
				return m
			}
			// 진행 메시지 처리 후 다음 읽기 cmd는 Update가 반환한다.
			continue
		}
		next, c := m.Update(msg)
		m = next.(Model)
		cmd = c
	}
	_ = sawProgress
	return m
}

// loadPanes는 양쪽 패널 목록을 로드한다.
func loadPanes(m Model) Model {
	for _, pane := range []int{0, 1} {
		next, _ := m.Update(listCmd(&m, pane)())
		m = next.(Model)
	}
	return m
}

// TestPasteSelfCopySkipped는 y(yank) → p(붙여넣기) 전체 흐름을 시뮬레이션한다.
// 양쪽 패널이 같은 폴더인 경우(버그 재현 조건) 붙여넣기가 건너뛰고
// 원본 파일이 0바이트로 덮이지 않아야 한다.
func TestPasteSelfCopySkipped(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(sub, "data.txt")
	if err := os.WriteFile(fp, []byte("hello-world"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(sub, sub) // 좌우 같은 폴더 → 버그 재현 조건
	m.width, m.height = 200, 40
	m = loadPanes(m)

	idx := -1
	for i, e := range m.left.entries {
		if !e.IsDir && e.Name == "data.txt" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("data.txt가 목록에 없음")
	}
	m.left.cursor = idx

	next, _ := m.Update(key("y"))
	m = next.(Model)
	if len(m.yankPaths) != 1 || m.yankPaths[0] != fp {
		t.Fatalf("yank 실패: paths=%v", m.yankPaths)
	}

	next, cmd := m.Update(key("p"))
	if cmd == nil {
		t.Fatal("p에서 붙여넣기 cmd 없음")
	}
	m = next.(Model)
	m = runOpToCompletion(t, m, cmd)

	got, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello-world" {
		t.Fatalf("자기복사로 원본 손상: %q", got)
	}
	if !strings.Contains(m.message, "건너뜀") {
		t.Fatalf("건너뜀 메시지 없음: %q", m.message)
	}
}

// TestPasteToOtherPane는 반대 패널(다른 경로)로의 붙여넣기가 정상 동작하고,
// 완료 후 상태(진행/복사대기/체크박스)가 원복되는지 확인한다.
func TestPasteToOtherPane(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fp := filepath.Join(srcDir, "data.txt")
	if err := os.WriteFile(fp, []byte("hello-world"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(srcDir, dstDir)
	m.width, m.height = 200, 40
	m = loadPanes(m)

	idx := -1
	for i, e := range m.left.entries {
		if e.Name == "data.txt" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("data.txt가 목록에 없음")
	}
	m.left.cursor = idx

	// 선택모드 + Space로 마킹한 뒤에도 완료 후 체크박스가 원복되는지 확인
	next, _ := m.Update(key("v"))
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)
	if len(m.marks) != 1 {
		t.Fatalf("mark 실패: %v", m.marks)
	}

	next, _ = m.Update(key("y"))
	m = next.(Model)

	// Tab → 오른쪽(dstDir)으로 전환 후 p (붙여넣기)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.active != 1 {
		t.Fatalf("Tab 후 active=%d, want 1", m.active)
	}
	next, cmd := m.Update(key("p"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("p에서 붙여넣기 cmd 없음")
	}
	m = runOpToCompletion(t, m, cmd)

	got, err := os.ReadFile(filepath.Join(dstDir, "data.txt"))
	if err != nil {
		t.Fatalf("반대 패널로 복사 실패: %v", err)
	}
	if string(got) != "hello-world" {
		t.Fatalf("복사 내용 불일치: %q", got)
	}

	// 완료 후 상태 원복 확인
	if len(m.yankPaths) != 0 || m.yankFS != nil {
		t.Fatalf("완료 후 복사대기 미해제: paths=%v fs=%v", m.yankPaths, m.yankFS)
	}
	if len(m.marks) != 0 {
		t.Fatalf("완료 후 체크박스 미해제: %v", m.marks)
	}
	if m.working != "" {
		t.Fatalf("완료 후 진행상태 미해제: %q", m.working)
	}
	if !strings.Contains(m.message, "완료") {
		t.Fatalf("완료 메시지 없음: %q", m.message)
	}
}

// TestMoveResetsState는 m(이동) 완료 후 색상/체크박스가 원복되는지 확인한다.
func TestMoveResetsState(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fp := filepath.Join(srcDir, "move.txt")
	if err := os.WriteFile(fp, []byte("move-me"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(srcDir, dstDir)
	m.width, m.height = 200, 40
	m = loadPanes(m)

	idx := -1
	for i, e := range m.left.entries {
		if e.Name == "move.txt" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("move.txt가 목록에 없음")
	}
	m.left.cursor = idx

	next, cmd := m.Update(key("m"))
	if cmd == nil {
		t.Fatal("m에서 이동 cmd 없음")
	}
	m = next.(Model)
	m = runOpToCompletion(t, m, cmd)

	if _, err := os.Stat(filepath.Join(dstDir, "move.txt")); err != nil {
		t.Fatalf("이동 실패: %v", err)
	}
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Fatalf("원본이 아직 존재함")
	}
	if len(m.yankPaths) != 0 || m.yankFS != nil {
		t.Fatalf("이동 후 복사대기 미해제: %v %v", m.yankPaths, m.yankFS)
	}
	if len(m.marks) != 0 {
		t.Fatalf("이동 후 체크박스 미해제: %v", m.marks)
	}
}
