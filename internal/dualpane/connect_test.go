package dualpane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// TestKeysRebound는 v=체크박스, s=접속 폼으로 바뀌었는지 확인한다.
func TestKeysRebound(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 120, 30

	// v: 선택모드 토글
	next, _ := m.Update(runeKey("v"))
	m = next.(Model)
	if !m.left.selMode {
		t.Fatalf("v로 선택모드가 켜지지 않음")
	}
	next, _ = m.Update(runeKey("v"))
	m = next.(Model)
	if m.left.selMode {
		t.Fatalf("v로 선택모드가 꺼지지 않음")
	}

	// s: 접속 폼 열기 (선택모드가 켜지면 안 됨)
	next, _ = m.Update(runeKey("s"))
	m = next.(Model)
	if !m.connMode {
		t.Fatalf("s로 접속 폼이 열리지 않음")
	}
	if m.left.selMode {
		t.Fatalf("s가 선택모드를 건드림")
	}
}

// TestConnFormFields는 폼에서 호스트/아이디/비밀번호를 입력하고 필드를 이동하는지 확인한다.
func TestConnFormFields(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 140, 30

	next, _ := m.Update(runeKey("s"))
	m = next.(Model)
	m.connHost, m.connUser, m.connPass = "", "", "" // 초기화(USER 프리필 제거)
	m.connField = 0

	// 호스트 입력
	for _, ch := range "10.0.0.5" {
		next, _ = m.Update(runeKey(string(ch)))
		m = next.(Model)
	}
	// Tab → 아이디
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.connField != 1 {
		t.Fatalf("Tab 후 필드=%d, want 1", m.connField)
	}
	for _, ch := range "root" {
		next, _ = m.Update(runeKey(string(ch)))
		m = next.(Model)
	}
	// Enter → 비밀번호
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.connField != 2 {
		t.Fatalf("Enter 후 필드=%d, want 2", m.connField)
	}
	for _, ch := range "s3cr3t" {
		next, _ = m.Update(runeKey(string(ch)))
		m = next.(Model)
	}

	if m.connHost != "10.0.0.5" || m.connUser != "root" || m.connPass != "s3cr3t" {
		t.Fatalf("필드 입력 오류 host=%q user=%q pass=%q", m.connHost, m.connUser, m.connPass)
	}

	// 렌더에 세 라벨과 마스킹이 보이는지
	v := m.View()
	for _, s := range []string{"IP/호스트", "아이디", "비밀번호", "••••••"} {
		if !strings.Contains(v, s) {
			t.Errorf("접속 폼 렌더에 %q 없음", s)
		}
	}
	if strings.Contains(v, "s3cr3t") {
		t.Errorf("비밀번호가 평문으로 노출됨")
	}

	// esc 닫기
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.connMode {
		t.Fatalf("esc로 접속 폼이 닫히지 않음")
	}
}

// TestConnFormLocalPath는 호스트에 로컬 경로를 넣으면 (활성 패널과 무관하게)
// 오른쪽 패널로 전환되는지 확인한다.
func TestConnFormLocalPath(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	target := t.TempDir()

	m := New(left, right)
	m.width, m.height = 120, 30
	m = loadPanes(m)
	if m.active != 0 {
		t.Fatalf("초기 active=%d, want 0", m.active)
	}

	next, _ := m.Update(runeKey("s"))
	m = next.(Model)
	m.connHost, m.connUser, m.connPass = target, "", "" // 아이디/비밀번호 비우고 경로 입력
	m.connField = 2

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.connMode {
		t.Fatalf("연결 후에도 폼이 열려 있음")
	}
	if cmd == nil {
		t.Fatalf("로컬 전환 후 목록 갱신 cmd 없음")
	}
	// 목록 갱신 메시지 적용
	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.left.path != left {
		t.Errorf("왼쪽 패널이 변경됨: %q (want %q)", m.left.path, left)
	}
	if m.right.path != target {
		t.Errorf("오른쪽 패널로 전환되지 않음: %q (want %q)", m.right.path, target)
	}
}

// TestDisconnectRestores는 b로 접속을 끊으면 오른쪽 패널이 접속 전 상태로
// 복원되는지 확인한다.
func TestDisconnectRestores(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // SaveHost/LoadHosts 부작용 격리
	left, right := t.TempDir(), t.TempDir()
	m := New(left, right)
	m.width, m.height = 120, 30
	m = loadPanes(m)

	// 원격 접속 시뮬레이션
	addr := Addr{User: "u", Host: "h", Port: 22, Path: "/home/u", Raw: "u@h"}
	next, _ := m.Update(connectedMsg{pane: connPane, fs: LocalFS{}, addr: addr})
	m = next.(Model)
	if m.backup[connPane] == nil {
		t.Fatalf("접속 시 원래 상태 백업이 안 됨")
	}
	if !m.right.remote || m.right.path != "/home/u" {
		t.Fatalf("원격 접속 상태가 아님: remote=%v path=%q", m.right.remote, m.right.path)
	}

	// b: 접속 종료
	next, cmd := m.Update(runeKey("b"))
	m = next.(Model)
	if m.right.remote {
		t.Fatalf("접속 종료 후에도 remote=true")
	}
	if m.right.path != right {
		t.Fatalf("원래 경로로 복원 안 됨: %q (want %q)", m.right.path, right)
	}
	if cmd == nil {
		t.Fatalf("접속 종료 후 목록 갱신 cmd 없음")
	}
	if m.backup[connPane] != nil {
		t.Fatalf("복원 후 백업이 남아 있음")
	}
	if m.left.path != left {
		t.Errorf("왼쪽 패널이 변경됨: %q", m.left.path)
	}
}

// TestDisconnectKeepsOriginalBackup은 재접속해도 최초(로컬) 백업이 유지되는지 확인한다.
func TestDisconnectKeepsOriginalBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	left, right := t.TempDir(), t.TempDir()
	m := New(left, right)
	m.width, m.height = 120, 30
	m = loadPanes(m)

	first := Addr{User: "a", Host: "h1", Port: 22, Path: "/a", Raw: "a@h1"}
	next, _ := m.Update(connectedMsg{pane: connPane, fs: LocalFS{}, addr: first})
	m = next.(Model)

	// 두 번째 접속 (이미 remote 상태)
	second := Addr{User: "b", Host: "h2", Port: 22, Path: "/b", Raw: "b@h2"}
	next, _ = m.Update(connectedMsg{pane: connPane, fs: LocalFS{}, addr: second})
	m = next.(Model)

	// b → 최초 로컬 백업으로 복원되어야 함
	next, _ = m.Update(runeKey("b"))
	m = next.(Model)
	if m.right.path != right {
		t.Fatalf("재접속 후 최초 백업으로 복원 안 됨: %q (want %q)", m.right.path, right)
	}
}

// TestVolumeMenu는 외장디스크 메뉴의 이동/선택 동작을 확인한다.
func TestVolumeMenu(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	m := New(left, right)
	m.width, m.height = 120, 30
	m = loadPanes(m)
	m.active = 0

	m.volMode = true
	m.volList = []string{"/Volumes/SSD1", "/Volumes/SSD2"}
	m.volCursor = 0

	// 렌더 확인
	if v := m.View(); !strings.Contains(v, "외장 디스크 선택") || !strings.Contains(v, "SSD2") {
		t.Fatalf("외장디스크 메뉴 렌더 오류")
	}

	// 아래로 이동
	next, _ := m.Update(runeKey("j"))
	m = next.(Model)
	if m.volCursor != 1 {
		t.Fatalf("커서 이동 실패: %d", m.volCursor)
	}

	// esc 닫기
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.volMode {
		t.Fatalf("esc로 메뉴가 닫히지 않음")
	}

	// 다시 열어 선택
	m.volMode = true
	m.volCursor = 1
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.volMode {
		t.Fatalf("선택 후에도 메뉴가 열려 있음")
	}
	if m.right.path != "/Volumes/SSD2" {
		t.Fatalf("오른쪽 패널로 외장디스크가 열리지 않음: %q", m.right.path)
	}
	if cmd == nil {
		t.Fatalf("선택 후 목록 갱신 cmd 없음")
	}
}

// TestVolumeBackupRevert는 외장디스크로 연 뒤 b로 원래 상태로 되돌아오는지 확인한다.
func TestVolumeBackupRevert(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	m := New(left, right)
	m.width, m.height = 120, 30
	m = loadPanes(m)
	m.active = 0

	// 외장디스크 선택
	m.volMode = true
	m.volList = []string{"/Volumes/SSD1"}
	m.volCursor = 0
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.right.path != "/Volumes/SSD1" {
		t.Fatalf("외장디스크가 열리지 않음: %q", m.right.path)
	}
	_ = cmd // 목록 갱신 cmd 소비는 생략 (경로 미존재 허용)

	// b: 원복
	next, cmd = m.Update(runeKey("b"))
	m = next.(Model)
	if m.right.path != right {
		t.Fatalf("b로 원래 경로 복원 안 됨: %q (want %q)", m.right.path, right)
	}
	if m.backup[connPane] != nil {
		t.Fatalf("복원 후 백업이 남아 있음")
	}
	if cmd == nil {
		t.Fatalf("원복 후 목록 갱신 cmd 없음")
	}
}
