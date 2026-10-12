package dualpane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func escKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func leftKey() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyLeft} }

// drainCmd는 목록 갱신 cmd를 실행해 모델에 반영한다.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatalf("목록 갱신 cmd 없음")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// TestClipboardProcessFixed는 Clipboard|Process 분할 고정 4줄 푸터에서
// 복사대기 유무와 무관하게 화면 전체가 터미널 높이에 정확히 맞는지 확인한다.
func TestClipboardProcessFixed(t *testing.T) {
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

		// 복사대기 2개 (푸터는 고정 4줄이라 목록 영역 불변)
		m.yankFS = LocalFS{}
		m.yankPaths = []string{filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")}
		v := m.View()
		on := strings.Count(v, "\n")

		if off != on {
			t.Errorf("h=%d: 복사대기 유무에 따라 높이가 달라짐 (off=%d, on=%d)", h, off, on)
		}
		if on != h-1 {
			t.Errorf("h=%d: 화면 높이 이탈 (newlines=%d, want %d)", h, on, h-1)
		}
		if !strings.Contains(v, "Clipboard") {
			t.Errorf("푸터에 Clipboard 창 없음")
		}
		if !strings.Contains(v, "Process") {
			t.Errorf("푸터에 Process 창 없음")
		}
		if !strings.Contains(v, "선택") {
			t.Errorf("푸터에 항목수 줄 없음")
		}
		if !strings.Contains(v, "복사 대기 2개") {
			t.Errorf("Clipboard에 복사 대기 목록 없음")
		}
	}
}

// TestGotoRootAndHelp는 / 이동(/ 루트 포함)과 /?·/h 도움말을 확인한다.
func TestGotoRootAndHelp(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(sub, sub)
	m.width, m.height = 120, 30
	m = loadPanes(m)

	// / 키로 이동 프롬프트 진입 ("/" 프리필)
	next, _ := m.Update(runeKey("/"))
	m = next.(Model)
	if !m.gotoMode || m.gotoInput != "/" {
		t.Fatalf("goto 진입 실패: mode=%v input=%q", m.gotoMode, m.gotoInput)
	}

	// /? 입력 후 Enter → 도움말
	m.gotoInput = "/?"
	next, _ = m.Update(enterKey())
	m = next.(Model)
	if m.gotoMode || !m.helpMode {
		t.Fatalf("/? 후 도움말 미표시: goto=%v help=%v", m.gotoMode, m.helpMode)
	}
	if v := m.View(); !strings.Contains(v, "도움말") {
		t.Fatalf("도움말 렌더 없음")
	}
	// esc로 닫기
	next, _ = m.Update(escKey())
	m = next.(Model)
	if m.helpMode {
		t.Fatalf("esc로 도움말이 닫히지 않음")
	}

	// /h 입력 후 Enter → 도움말
	next, _ = m.Update(runeKey("/"))
	m = next.(Model)
	m.gotoInput = "/h"
	next, _ = m.Update(enterKey())
	m = next.(Model)
	if !m.helpMode {
		t.Fatalf("/h 후 도움말 미표시")
	}
	next, _ = m.Update(escKey())
	m = next.(Model)

	// / 루트 이동 (활성 패널 기준)
	next, _ = m.Update(runeKey("/"))
	m = next.(Model)
	m.gotoInput = "/"
	next, cmd := m.Update(enterKey())
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("/ 이동 후 목록 갱신 cmd 없음")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.pane(m.active).path != "/" {
		t.Fatalf("/ 루트 이동 실패: %q", m.pane(m.active).path)
	}

	// ? 단독 키로도 도움말
	next, _ = m.Update(runeKey("?"))
	m = next.(Model)
	if !m.helpMode {
		t.Fatalf("? 키로 도움말 미표시")
	}
}

// TestBottomHintsRemoved는 하단 상시 키 설명이 사라졌는지 확인한다.
func TestBottomHintsRemoved(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 120, 30
	m = loadPanes(m)
	v := m.View()
	for _, s := range []string{"Tab 전환", "숨김파일", "붙이기"} {
		if strings.Contains(v, s) {
			t.Errorf("하단 상시 설명이 남아 있음: %q", s)
		}
	}
	if !strings.Contains(v, "/? 도움말") {
		t.Errorf("헤더에 /? 안내 없음")
	}
}

// TestArrowUpToRoot는 ←(화살표)만으로 상위로 올라가 / 루트까지
// 도달하는지 확인한다 (첫 ←부터 바로 상위 이동).
func TestArrowUpToRoot(t *testing.T) {
	root := t.TempDir()
	sub1 := filepath.Join(root, "sub1")
	sub2 := filepath.Join(sub1, "sub2")
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(sub2, sub2)
	m.width, m.height = 120, 30
	m = loadPanes(m)

	// 첫 ←: 바로 상위(sub1)로 이동해야 함 (제자리 소모 금지)
	next, cmd := m.Update(leftKey())
	m = drainCmd(t, next.(Model), cmd)
	if m.pane(m.active).path != sub1 {
		t.Fatalf("첫 ← 후 경로=%q, want %q", m.pane(m.active).path, sub1)
	}

	// ← 반복으로 / 루트까지 도달
	for i := 0; i < 32; i++ {
		if m.pane(m.active).path == "/" {
			break
		}
		next, cmd := m.Update(leftKey())
		m = next.(Model)
		if cmd != nil {
			next, _ := m.Update(cmd())
			m = next.(Model)
		}
	}
	if m.pane(m.active).path != "/" {
		t.Fatalf("/ 루트 도달 실패: %q", m.pane(m.active).path)
	}

	// / 에서 ←는 제자리 (무한루프·에러 없음)
	next, cmd = m.Update(leftKey())
	m = next.(Model)
	if cmd != nil {
		t.Fatalf("/ 에서 ←가 이동 cmd를 반환함")
	}
	if m.pane(m.active).path != "/" {
		t.Fatalf("/ 에서 ← 후 경로 변경: %q", m.pane(m.active).path)
	}
}

// TestHeaderShowsCurrentPath는 상단 헤더와 패널 제목이 시작 경로가 아닌
// 현재 위치를 표시하는지 확인한다.
func TestHeaderShowsCurrentPath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(root, root)
	m.width, m.height = 140, 30
	m = loadPanes(m)

	// / 프롬프트로 sub 이동
	next, _ := m.Update(runeKey("/"))
	m = next.(Model)
	m.gotoInput = sub
	next, cmd := m.Update(enterKey())
	m = drainCmd(t, next.(Model), cmd)

	if got := m.addrLine(); !strings.Contains(got, sub) {
		t.Errorf("헤더에 현재 위치 없음: %q (want %q 포함)", got, sub)
	}
	if v := m.View(); !strings.Contains(v, sub) {
		t.Errorf("화면에 현재 위치 없음 (want %q)", sub)
	}
}

// TestHelpEnterCloses는 도움말에서 Enter를 치면 원래 화면으로
// 돌아오는지 확인한다.
func TestHelpEnterCloses(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 120, 30
	m = loadPanes(m)

	next, _ := m.Update(runeKey("?"))
	m = next.(Model)
	if !m.helpMode {
		t.Fatalf("? 키로 도움말 미표시")
	}
	if v := m.View(); !strings.Contains(v, "도움말") {
		t.Fatalf("도움말 렌더 없음")
	}

	next, _ = m.Update(enterKey())
	m = next.(Model)
	if m.helpMode {
		t.Fatalf("Enter로 도움말이 닫히지 않음")
	}
	if v := m.View(); !strings.Contains(v, "Clipboard") || !strings.Contains(v, "Process") {
		t.Fatalf("도움말 종료 후 원래 화면(Clipboard/Process) 아님")
	}
}

// TestHelpTwoLines는 도움말 본문이 2줄이고 모달 안에 들어가는지 확인한다.
func TestHelpTwoLines(t *testing.T) {
	hl := helpLines()
	if len(hl) != 2 {
		t.Fatalf("도움말 본문이 2줄이 아님: %d줄", len(hl))
	}
	for i, l := range hl {
		// ↑↓←·는 터미널 1셀이므로 보정 후 비교
		real := runewidth.StringWidth(l)
		for _, r := range []string{"↑", "↓", "←", "·"} {
			real -= strings.Count(l, r)
		}
		if real > 96 {
			t.Errorf("본문 %d줄이 너무 김: real=%d (%q)", i+1, real, l)
		}
	}

	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 120, 30
	m = loadPanes(m)
	next, _ := m.Update(runeKey("?"))
	m = next.(Model)
	v := m.View()
	if !strings.Contains(v, "╔") || !strings.Contains(v, "╝") {
		t.Errorf("두줄 테두리 모달 아님")
	}
	for _, want := range []string{"Tab 전환", "붙이기", "외장", "Enter/esc 닫기", "빌드 " + BuildTag} {
		if !strings.Contains(v, want) {
			t.Errorf("도움말에 %q 없음", want)
		}
	}
}

// TestHeaderSingleLine은 경로가 매우 길어도 헤더가 1줄을 유지해
// 전체 높이가 터미널을 초과하지 않는지 확인한다.
func TestHeaderSingleLine(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 100, 30
	m = loadPanes(m)
	m.left.path = "/" + strings.Repeat("아주긴폴더이름", 30)
	m.right.path = "/" + strings.Repeat("b", 150)
	v := m.View()
	if got := strings.Count(v, "\n"); got != m.height-1 {
		t.Errorf("긴 경로에서 높이 이탈: newlines=%d, want %d", got, m.height-1)
	}
}

// TestOpHistory는 작업 완료 후 우측 프로세스칸에 최근 기록이
// 쌓이는지(최근/이전) 확인한다.
func TestOpHistory(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fp := filepath.Join(srcDir, "data.txt")
	if err := os.WriteFile(fp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(srcDir, dstDir)
	m.width, m.height = 200, 40
	m = loadPanes(m)
	for i, e := range m.left.entries {
		if e.Name == "data.txt" {
			m.left.cursor = i
		}
	}

	// 붙여넣기 → 기록 1개
	next, _ := m.Update(key("y"))
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	next, cmd := m.Update(key("p"))
	m = next.(Model)
	m = runOpToCompletion(t, m, cmd)
	m = loadPanes(m) // 완료 후 재조회 (헬퍼는 done에서 멈추므로 직접 갱신)
	if len(m.opHist) != 1 || !strings.Contains(m.opHist[0], "완료") {
		t.Fatalf("기록 1개 오류: %v", m.opHist)
	}
	if v := m.View(); !strings.Contains(v, "최근: ") {
		t.Fatalf("푸터에 최근 기록 없음")
	}

	// 삭제 → 기록 2개 (최근/이전)
	next, _ = m.Update(key("d"))
	m = next.(Model)
	next, delCmd := m.Update(key("y"))
	m = next.(Model)
	if delCmd == nil {
		t.Fatalf("삭제 cmd 없음")
	}
	next, _ = m.Update(delCmd())
	m = next.(Model)
	if len(m.opHist) != 2 {
		t.Fatalf("기록 2개 오류: %v", m.opHist)
	}
	if v := m.View(); !strings.Contains(v, "이전: ") {
		t.Fatalf("푸터에 이전 기록 없음")
	}
}

// TestProgressPercent는 파일별 진행률과 바 표시를 확인한다.
func TestProgressPercent(t *testing.T) {
	// 현재 파일 45/100 → 45%
	s := opState{kind: "paste", item: 2, total: 3, totalByts: 100, fileDone: 45, current: "b.iso"}
	txt := progressText(s)
	if !strings.Contains(txt, "45%") {
		t.Errorf("파일별 진행률 없음: %q", txt)
	}
	if !strings.Contains(txt, "[====>     ]") {
		t.Errorf("진행 바 오류: %q", txt)
	}
	if !strings.Contains(txt, "2/3") || !strings.Contains(txt, "b.iso") {
		t.Errorf("항목 표시 누락: %q", txt)
	}
	// 파일 크기를 모르면 진행률 없음 (토탈 표시 금지)
	s = opState{kind: "paste", item: 2, total: 3, totalByts: -1, current: "b.iso"}
	if txt := progressText(s); strings.Contains(txt, "%") {
		t.Errorf("불확실한 진행률이 표시됨: %q", txt)
	}
	// 초과분 클램프
	s = opState{kind: "paste", item: 1, total: 1, totalByts: 100, fileDone: 150}
	if txt := progressText(s); !strings.Contains(txt, "100%") {
		t.Errorf("클램프 오류: %q", txt)
	}
	// 바 폭 정확 (ASCII)
	if got := barStr(0, 10); got != "[>         ]" {
		t.Errorf("0%% 바 오류: %q", got)
	}
	if got := barStr(100, 10); got != "[==========]" {
		t.Errorf("100%% 바 오류: %q", got)
	}
	if got := barStr(150, 10); got != "[==========]" {
		t.Errorf("초과 바 오류: %q", got)
	}
}

// TestSizeOf는 진행률용 단일 파일 크기 조회를 확인한다.
func TestSizeOf(t *testing.T) {
	root := t.TempDir()
	fp := filepath.Join(root, "f.bin")
	if err := os.WriteFile(fp, make([]byte, 12345), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SizeOf(LocalFS{}, fp); got != 12345 {
		t.Errorf("SizeOf=%d, want 12345", got)
	}
	if got := SizeOf(LocalFS{}, filepath.Join(root, "none")); got != -1 {
		t.Errorf("없는 파일 SizeOf=%d, want -1", got)
	}
	if got := SizeOf(LocalFS{}, root); got != -1 {
		t.Errorf("폴더 SizeOf=%d, want -1", got)
	}
}

// TestSingleFileByteTotal은 단일 파일 복사 시작 시 전체 바이트가
// 진행 메시지에 실리는지 확인한다.
func TestSingleFileByteTotal(t *testing.T) {
	root := t.TempDir()
	fp := filepath.Join(root, "big.bin")
	if err := os.WriteFile(fp, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	ch := make(chan opMsg, 128)
	cmd := runOp("paste", LocalFS{}, []string{fp}, LocalFS{}, dst, ch)
	msg := cmd()
	om, ok := msg.(opMsg)
	if !ok || om.done {
		t.Fatalf("첫 진행 메시지 없음: %T", msg)
	}
	if om.state.totalByts != 100 {
		t.Errorf("totalByts=%d, want 100", om.state.totalByts)
	}
	for m := range ch {
		if m.done && !strings.Contains(m.desc, "완료") {
			t.Errorf("완료 메시지 오류: %q", m.desc)
		}
		if m.done {
			break
		}
	}
}

// TestMultiFilePerFile는 여러 파일 복사 시 각 파일의 크기 정보가
// 진행 메시지에 실리는지(파일별 진행률용) 확인한다.
func TestMultiFilePerFile(t *testing.T) {
	root := t.TempDir()
	var paths []string
	for _, n := range []string{"a.bin", "b.bin"} {
		fp := filepath.Join(root, n)
		if err := os.WriteFile(fp, make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, fp)
	}
	dst := t.TempDir()
	ch := make(chan opMsg, 128)
	cmd := runOp("paste", LocalFS{}, paths, LocalFS{}, dst, ch)
	first := cmd()
	om, ok := first.(opMsg)
	if !ok || om.done {
		t.Fatalf("첫 진행 메시지 없음: %T", first)
	}
	if om.state.totalByts != -1 {
		t.Errorf("시작 메시지에 파일 크기 있으면 안 됨: %d", om.state.totalByts)
	}
	sawFile := false
	for m := range ch {
		if m.done {
			break
		}
		if m.state.totalByts == 100 && (m.state.current == "a.bin" || m.state.current == "b.bin") {
			sawFile = true
		}
	}
	if !sawFile {
		t.Errorf("파일별 크기 정보 없음")
	}
}

// TestDisplayExact는 Ambiguous 문자(–·…▶→ 등)가 폭 측정 경로에서
// ASCII로 정규화되는지 확인한다 (ko 로케일 2셀 측정 → 박스 넘침 방지).
func TestDisplayExact(t *testing.T) {
	if got := disp("a–b—c·d…e▶f→g"); got != "a-b-c-d...e>f->g" {
		t.Errorf("disp 오류: %q", got)
	}
	if got := disp("a\tb\nc\rd"); got != "a b c d" {
		t.Errorf("제어문자 정규화 오류: %q", got)
	}
	for _, fn := range []func(string, int) string{padW, padCell, shorten} {
		out := fn("ubuntu–26.04.1–desktop–amd64.iso", 20)
		for _, r := range []string{"–", "—", "·", "…", "▶", "→", "│", "─"} {
			if strings.Contains(out, r) {
				t.Errorf("정규화 누락: %q에 %q 남음", out, r)
			}
		}
	}
	// 잘린 결과가 폭을 초과하지 않는지
	out := padCell("ubuntu–26.04.1–desktop–amd64.iso", 15)
	if w := runewidth.StringWidth(stripANSI(out)); w > 15 {
		t.Errorf("padCell 폭 초과: %d > 15 (%q)", w, out)
	}
}

// TestEnDashRowAligned는 –가 든 파일명 행이 박스를 넘지 않는지 확인한다
// (ISO 파일명처럼 en dash가 여러 개여도 정규화되어 정렬 유지).
func TestEnDashRowAligned(t *testing.T) {
	root := t.TempDir()
	names := []string{
		"elementaryos–8.1–stable–amd64.20260219.iso",
		"linuxmint–22.3–cinnamon–64bit–hwe–6.17.iso",
		"ubuntu–26.04.1–desktop–amd64.iso",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(root, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root, root)
	m.width, m.height = 120, 30
	m = loadPanes(m)
	// 선택모드+커서로 스크린샷 조건 재현
	next, _ := m.Update(runeKey("v"))
	m = next.(Model)
	v := m.View()
	if strings.Contains(v, "–") {
		t.Errorf("정규화되지 않은 –가 화면에 남음")
	}
	for _, n := range []string{"elementaryos-8.1", "linuxmint-22.3", "ubuntu-26.04"} {
		if !strings.Contains(v, n) {
			t.Errorf("파일명 표시 누락: %q", n)
		}
	}
}

// TestBottomBoxesSeparated는 하단이 위 네비게이션처럼 테두리 분리 박스
// 2개인지(상단1+중앙2+하단2 = 윗변 5개) 확인한다.
func TestBottomBoxesSeparated(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 100, 30
	m = loadPanes(m)
	v := m.View()
	if got := strings.Count(v, "╭"); got != 5 {
		t.Errorf("분리 박스 윗변=%d, want 5 (상단1+중앙2+하단2)", got)
	}
	if got := strings.Count(v, "╰"); got != 5 {
		t.Errorf("분리 박스 아랫변=%d, want 5", got)
	}
}

// TestDeleteDoubleD는 d 삭제 팝업에서 d를 눌러 삭제가 실행되는지 확인한다.
func TestDeleteDoubleD(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // 휴지통 격리
	root := t.TempDir()
	fp := filepath.Join(root, "gone.txt")
	if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root, root)
	m.width, m.height = 120, 30
	m = loadPanes(m)
	for i, e := range m.left.entries {
		if e.Name == "gone.txt" {
			m.left.cursor = i
		}
	}

	next, _ := m.Update(key("d"))
	m = next.(Model)
	if m.modal == nil || m.modal.kind != "delete" {
		t.Fatalf("삭제 팝업 없음: %+v", m.modal)
	}
	if v := m.View(); !strings.Contains(v, "삭제하겠습니까?") {
		t.Fatalf("삭제 팝업 렌더 없음")
	}
	next, delCmd := m.Update(key("d"))
	m = next.(Model)
	if delCmd == nil {
		t.Fatalf("두 번째 d에서 삭제 cmd 없음")
	}
	next, _ = m.Update(delCmd())
	m = next.(Model)

	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Fatalf("파일이 삭제되지 않음: %v", err)
	}
	if m.modal != nil {
		t.Fatalf("팝업 미해제: %+v", m.modal)
	}
	if len(m.opHist) != 1 || !strings.Contains(m.opHist[0], "휴지통") {
		t.Fatalf("삭제 기록 없음: %v", m.opHist)
	}
}

// TestDeleteEscCancels는 d 후 esc로 취소되면 파일이 남는지 확인한다.
func TestDeleteEscCancels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	fp := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root, root)
	m.width, m.height = 120, 30
	m = loadPanes(m)

	next, _ := m.Update(key("d"))
	m = next.(Model)
	if m.modal == nil {
		t.Fatalf("삭제 팝업 없음")
	}
	next, _ = m.Update(escKey())
	m = next.(Model)

	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("취소됐는데 파일이 없음: %v", err)
	}
	if m.modal != nil {
		t.Fatalf("팝업 미해제: %+v", m.modal)
	}
}

// TestOverwritePrompt는 붙여넣기 대상에 동명 파일이 있으면
// 덮어쓰기 팝업이 뜨는지 확인한다.
func TestOverwritePrompt(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "same.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "same.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(srcDir, dstDir)
	m.width, m.height = 200, 40
	m = loadPanes(m)
	for i, e := range m.left.entries {
		if e.Name == "same.txt" {
			m.left.cursor = i
		}
	}

	next, _ := m.Update(key("y"))
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	next, cmd := m.Update(key("p"))
	m = next.(Model)
	if cmd != nil {
		t.Fatalf("충돌 시 바로 실행되면 안 됨")
	}
	if m.modal == nil || m.modal.kind != "overwrite" {
		t.Fatalf("덮어쓰기 팝업 없음: %+v", m.modal)
	}
	if v := m.View(); !strings.Contains(v, "덮어쓰겠습니까?") {
		t.Fatalf("덮어쓰기 팝업 렌더 없음")
	}
}

// TestOverwriteDecide는 팝업에서 y(덮어쓰기)/s(건너뛰기)/n(취소)이
// 의도대로 동작하는지 확인한다.
func TestOverwriteDecide(t *testing.T) {
	setup := func(t *testing.T) Model {
		root := t.TempDir()
		srcDir := filepath.Join(root, "src")
		dstDir := filepath.Join(root, "dst")
		for _, d := range []string{srcDir, dstDir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(srcDir, "same.txt"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "fresh.txt"), []byte("fresh"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, "same.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := New(srcDir, dstDir)
		m.width, m.height = 200, 40
		m = loadPanes(m)
		// 두 파일 모두 yank (선택모드+Space)
		next, _ := m.Update(key("v"))
		m = next.(Model)
		for range m.left.entries {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
			m = next.(Model)
			next, _ = m.Update(key("j"))
			m = next.(Model)
		}
		next, _ = m.Update(key("y"))
		m = next.(Model)
		if len(m.yankPaths) != 2 {
			t.Fatalf("yank 2개 실패: %v", m.yankPaths)
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = next.(Model)
		return m
	}
	openModal := func(t *testing.T, m Model) (Model, string) {
		t.Helper()
		next, cmd := m.Update(key("p"))
		m = next.(Model)
		if cmd != nil {
			t.Fatalf("충돌 시 바로 실행되면 안 됨")
		}
		if m.modal == nil || m.modal.kind != "overwrite" {
			t.Fatalf("덮어쓰기 팝업 없음")
		}
		return m, m.modal.dstDir
	}

	// n=취소: 아무것도 안 바뀜
	m := setup(t)
	m, dstDir := openModal(t, m)
	next, _ := m.Update(key("n"))
	m = next.(Model)
	if m.modal != nil {
		t.Fatalf("팝업 미해제")
	}
	if got, _ := os.ReadFile(filepath.Join(dstDir, "same.txt")); string(got) != "old" {
		t.Fatalf("취소됐는데 덮임: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "fresh.txt")); !os.IsNotExist(err) {
		t.Fatalf("취소됐는데 복사됨")
	}

	// s=건너뛰기: fresh만 복사, same 유지
	m = setup(t)
	m, dstDir = openModal(t, m)
	next, cmd := m.Update(key("s"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("s에서 진행 cmd 없음")
	}
	m = runOpToCompletion(t, m, cmd)
	if got, _ := os.ReadFile(filepath.Join(dstDir, "same.txt")); string(got) != "old" {
		t.Fatalf("건너뛰기 실패 (덮임): %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dstDir, "fresh.txt")); string(got) != "fresh" {
		t.Fatalf("fresh 미복사: %q", got)
	}

	// y=덮어쓰기: 둘 다 반영
	m = setup(t)
	m, dstDir = openModal(t, m)
	next, cmd = m.Update(key("y"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("y에서 진행 cmd 없음")
	}
	m = runOpToCompletion(t, m, cmd)
	if got, _ := os.ReadFile(filepath.Join(dstDir, "same.txt")); string(got) != "new" {
		t.Fatalf("덮어쓰기 실패: %q", got)
	}
}

// TestNoPromptWhenNew는 겹치는 파일이 없으면 바로 실행되는지 확인한다.
func TestNoPromptWhenNew(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(srcDir, dstDir)
	m.width, m.height = 200, 40
	m = loadPanes(m)
	next, _ := m.Update(key("y"))
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	next, cmd := m.Update(key("p"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("신규 파일인데 진행 cmd 없음")
	}
	if m.modal != nil {
		t.Fatalf("팝업이 뜨면 안 됨: %+v", m.modal)
	}
	m = runOpToCompletion(t, m, cmd)
	if _, err := os.Stat(filepath.Join(dstDir, "new.txt")); err != nil {
		t.Fatalf("복사 실패: %v", err)
	}
}

// TestSameLocationNoPrompt는 같은 폴더 붙여넣기(전부 건너뜀)에
// 팝업이 뜨지 않는지 확인한다.
func TestSameLocationNoPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root, root)
	m.width, m.height = 200, 40
	m = loadPanes(m)
	next, _ := m.Update(key("y"))
	m = next.(Model)
	next, cmd := m.Update(key("p"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("진행 cmd 없음")
	}
	if m.modal != nil {
		t.Fatalf("자기복사에 팝업이 뜨면 안 됨: %+v", m.modal)
	}
}

// TestMoveOverwritePrompt는 이동(m) 시에도 충돌 팝업이 뜨는지 확인한다.
func TestMoveOverwritePrompt(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "same.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "same.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(srcDir, dstDir)
	m.width, m.height = 200, 40
	m = loadPanes(m)
	next, cmd := m.Update(key("m"))
	m = next.(Model)
	if cmd != nil {
		t.Fatalf("충돌 시 바로 실행되면 안 됨")
	}
	if m.modal == nil || m.modal.kind != "overwrite" || m.modal.opKind != "move" {
		t.Fatalf("이동 덮어쓰기 팝업 없음: %+v", m.modal)
	}
}

// TestPanesAligned는 좌박스를 줄인 좌우 폭 합이 상·하단 폭과 일치하고
// 좌박스가 우박스보다 작은지 확인한다.
func TestPanesAligned(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	m.width, m.height = 100, 30
	lw, rw := m.paneWidths()
	// 좌(+테두리2) + 구분자1 + 우(+테두리2) = 터미널폭
	if lw+2+1+rw+2 != m.width {
		t.Errorf("박스 폭 불일치: 좌내용=%d 우내용=%d 터미널=%d", lw, rw, m.width)
	}
	if lw >= rw {
		t.Errorf("좌박스가 줄지 않음: 좌=%d 우=%d", lw, rw)
	}

	// 화면 모든 줄의 실제 셀 폭이 터미널폭과 같은지
	// (박스 문자 │─╭╮╰╯는 터미널 1셀 → 측정값에서 개수만큼 보정)
	m = loadPanes(m)
	v := m.View()
	for _, l := range strings.Split(v, "\n") {
		plain := stripANSI(l)
		real := runewidth.StringWidth(plain)
		for _, r := range []string{"│", "─", "╭", "╮", "╰", "╯"} {
			real -= strings.Count(plain, r)
		}
		if real != m.width {
			t.Errorf("줄 폭 불일치: real=%d want=%d (%q)", real, m.width, plain)
		}
	}
}
