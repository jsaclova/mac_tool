package dualpane

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"fm/internal/human"
	"fm/internal/trash"
	"fm/internal/volumes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("15"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	checkStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	yankStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	headerBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("13")).Padding(0, 1)
	paneBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	footerBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
)

// Pane은 좌/우 한 패널이다.
type Pane struct {
	showDot bool
	selMode bool
	fs      FS
	label   string // 상단 주소 표시 (로컬 경로 or ssh 주소)
	remote  bool
	path    string
	entries []Entry
	cursor  int
	offset  int
	ready   bool
	errMsg  string
	history []string
}

type listedMsg struct {
	pane    int
	entries []Entry
	err     error
}

type doneOpMsg struct {
	desc string
	err  error
}

// opState는 진행 중인 복사/이동 상태다.
type opState struct {
	kind    string // "paste" | "move"
	item    int    // 현재 항목 (1-based)
	total   int    // 전체 항목 수
	files   int64  // 지금까지 처리한 파일 수
	byts    int64  // 지금까지 처리한 바이트
	current string // 현재 항목 이름
}

// opMsg는 복사/이동 진행 메시지다 (done=true면 종료).
type opMsg struct {
	state opState
	done  bool
	desc  string
	err   error
}

type connectedMsg struct {
	pane int
	fs   FS
	addr Addr
	err  error
}

type Model struct {
	left, right Pane
	active      int
	width       int
	height      int
	marks       map[string]bool
	yankFS      FS
	yankPaths   []string
	opCh        chan opMsg
	connMode    bool
	connField   int // 0=호스트(IP), 1=아이디, 2=비밀번호
	connHost    string
	connUser    string
	connPass    string
	addrHist    []string
	backup      [2]*Pane // 각 패널의 접속/연결 직전 상태 (b 로 원복)
	volMode     bool
	volList     []string
	volCursor   int
	working     string
	confirm     []string
	message     string
}

func New(leftPath, rightPath string) Model {
	m := Model{width: 160, height: 30, addrHist: LoadHosts(), marks: map[string]bool{}}
	m.left = Pane{fs: LocalFS{}, label: leftPath, path: leftPath, history: []string{leftPath}}
	m.right = Pane{fs: LocalFS{}, label: rightPath, path: rightPath, history: []string{rightPath}}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(listCmd(&m, 0), listCmd(&m, 1))
}

func listCmd(m *Model, pane int) tea.Cmd {
	var p *Pane
	if pane == 0 {
		p = &m.left
	} else {
		p = &m.right
	}
	fs, dir := p.fs, p.path
	return func() tea.Msg {
		ents, err := fs.ListDir(dir)
		if err == nil {
			sort.Slice(ents, func(i, j int) bool {
				if ents[i].IsDir != ents[j].IsDir {
					return ents[i].IsDir
				}
				return ents[i].Name < ents[j].Name
			})
		}
		return listedMsg{pane, ents, err}
	}
}

func (m *Model) pane(i int) *Pane {
	if i == 0 {
		return &m.left
	}
	return &m.right
}

// markKey는 패널+경로 고유 키다 (양쪽 같은 경로 대비).
func markKey(pane int, path string) string { return fmt.Sprintf("%d:%s", pane, path) }

// vis는 숨김파일 필터가 적용된 표시 목록이다.
func (m Model) vis(p *Pane, pane int) []Entry {
	_ = pane
	if p.showDot {
		return p.entries
	}
	out := p.entries[:0:0]
	for _, e := range p.entries {
		if !strings.HasPrefix(e.Name, ".") {
			out = append(out, e)
		}
	}
	return out
}

// marked는 활성 패널의 표시된 항목들이다.
func (m Model) marked() []Entry {
	p := m.pane(m.active)
	var out []Entry
	for _, e := range m.vis(p, m.active) {
		if m.marks[markKey(m.active, e.Path)] {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case listedMsg:
		p := m.pane(msg.pane)
		if msg.err != nil {
			p.errMsg = msg.err.Error()
			p.entries = nil
		} else {
			p.errMsg = ""
			p.entries = msg.entries
		}
		p.ready = true
		p.cursor, p.offset = 0, 0
		m.working = ""
		return m, nil
	case opMsg:
		if msg.done {
			m.opCh = nil
			m.working = ""
			if msg.err != nil {
				m.message = "실패: " + msg.err.Error()
			} else {
				m.message = msg.desc
			}
			// 완료 후 상태 원복: 체크박스 해제 + 복사대기(색상) 해제
			m.marks = map[string]bool{}
			m.yankFS = nil
			m.yankPaths = nil
			return m, tea.Batch(listCmd(&m, 0), listCmd(&m, 1))
		}
		m.working = progressText(msg.state)
		return m, readOp(m.opCh)
	case doneOpMsg:
		m.working = ""
		if msg.err != nil {
			m.message = "실패: " + msg.err.Error()
		} else {
			m.message = msg.desc
		}
		m.marks = map[string]bool{}
		return m, tea.Batch(listCmd(&m, 0), listCmd(&m, 1))
	case connectedMsg:
		m.working = ""
		if msg.err != nil {
			m.message = "접속 실패: " + msg.err.Error()
			return m, nil
		}
		p := m.pane(msg.pane)
		// 접속 전 상태를 백업 (b 로 원래대로 복원)
		if !p.remote {
			b := *p
			m.backup[msg.pane] = &b
		}
		_ = p.fs.Close()
		p.fs = msg.fs
		p.remote = true
		p.label = msg.addr.String()
		p.path = msg.addr.Path
		p.history = []string{p.path}
		SaveHost(msg.addr.Raw)
		m.addrHist = LoadHosts()
		m.message = "접속됨: " + msg.addr.String() + " (b 접속종료)"
		return m, listCmd(&m, msg.pane)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

// isYanked는 복사대기(y) 항목인지 판별한다.
func (m Model) isYanked(paneIdx int, ePath string) bool {
	if len(m.yankPaths) == 0 || m.yankFS == nil {
		return false
	}
	if !fsEqual(m.pane(paneIdx).fs, m.yankFS) {
		return false
	}
	for _, y := range m.yankPaths {
		if y == ePath {
			return true
		}
	}
	return false
}

func fsEqual(a, b FS) bool {
	_, al := a.(LocalFS)
	_, bl := b.(LocalFS)
	if al && bl {
		return true
	}
	sa, aok := a.(*SftpFS)
	sb, bok := b.(*SftpFS)
	return aok && bok && sa == sb
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.connMode {
		return m.updateConn(k)
	}
	if m.volMode {
		return m.updateVol(k)
	}
	if len(m.confirm) > 0 {
		if k == "y" || k == "Y" {
			targets := m.confirm
			m.confirm = nil
			p := m.pane(m.active)
			return m, m.deleteCmd(p, targets)
		}
		m.confirm = nil
		m.message = "취소됨"
		return m, nil
	}
	p := m.pane(m.active)
	rows := m.vis(p, m.active)
	switch k {
	case "q":
		_ = m.left.fs.Close()
		_ = m.right.fs.Close()
		return m, tea.Quit
	case "esc":
		if len(m.yankPaths) > 0 {
			m.yankFS = nil
			m.yankPaths = nil
			m.message = "복사대기 취소"
			return m, nil
		}
		if p.selMode {
			p.selMode = false
			m.message = "선택모드 해제"
			return m, nil
		}
	case "tab":
		m.active = 1 - m.active
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(rows)-1 {
			p.cursor++
		}
	case "enter", "right", "l":
		if len(rows) > 0 && p.cursor < len(rows) && rows[p.cursor].IsDir {
			e := rows[p.cursor]
			p.history = append(p.history, p.path)
			p.path = e.Path
			p.ready = false
			return m, listCmd(&m, m.active)
		}
	case "backspace", "left", "h":
		if n := len(p.history); n > 0 {
			p.path = p.history[n-1]
			p.history = p.history[:n-1]
			p.ready = false
			return m, listCmd(&m, m.active)
		}
		up := parentOf(p)
		if up != p.path {
			p.history = append(p.history, p.path)
			p.path = up
			p.ready = false
			return m, listCmd(&m, m.active)
		}
	case "a":
		p.showDot = !p.showDot
		if p.showDot {
			m.message = "숨김파일 표시 (이 패널만)"
		} else {
			m.message = "숨김파일 숨김"
		}
	case "v":
		p.selMode = !p.selMode
		if p.selMode {
			m.message = "선택모드: Space로 멀티 선택"
		} else {
			m.message = "선택모드 해제"
		}
	case " ":
		if p.selMode && len(rows) > 0 && p.cursor < len(rows) {
			key := markKey(m.active, rows[p.cursor].Path)
			if m.marks[key] {
				delete(m.marks, key)
			} else {
				m.marks[key] = true
			}
		}
		return m, nil
	case "s":
		return m.openConn()
	case "b":
		return m.disconnect()
	case "e":
		return m.openVolumes()
	case "y":
		srcs := m.marked()
		if len(srcs) == 0 && len(rows) > 0 && p.cursor < len(rows) {
			srcs = []Entry{rows[p.cursor]}
		}
		if len(srcs) == 0 {
			m.message = "복사할 항목 없음 (v 선택모드 + Space)"
			return m, nil
		}
		m.yankFS = p.fs
		m.yankPaths = m.yankPaths[:0]
		for _, s := range srcs {
			m.yankPaths = append(m.yankPaths, s.Path)
		}
		m.message = fmt.Sprintf("복사 대기: %d개 (반대 패널 이동 후 p 붙여넣기)", len(m.yankPaths))
		return m, nil
	case "p":
		if len(m.yankPaths) == 0 {
			m.message = "붙여넣을 항목 없음 (먼저 y 복사)"
			return m, nil
		}
		yankFS := m.yankFS
		paths := append([]string{}, m.yankPaths...)
		dst := m.pane(m.active)
		dstFS, dstDir := dst.fs, dst.path
		m.opCh = make(chan opMsg, 128)
		m.working = fmt.Sprintf("붙여넣기… 0/%d → %s", len(paths), dstDir)
		return m, runOp("paste", yankFS, paths, dstFS, dstDir, m.opCh)
	case "m":
		srcs := m.marked()
		if len(srcs) == 0 && len(rows) > 0 && p.cursor < len(rows) {
			srcs = []Entry{rows[p.cursor]}
		}
		if len(srcs) == 0 {
			m.message = "이동할 항목 없음"
			return m, nil
		}
		srcFS := p.fs
		paths := make([]string, 0, len(srcs))
		for _, s := range srcs {
			paths = append(paths, s.Path)
		}
		dst := m.pane(1 - m.active)
		dstFS, dstDir := dst.fs, dst.path
		m.opCh = make(chan opMsg, 128)
		m.working = fmt.Sprintf("이동… 0/%d → %s", len(paths), dstDir)
		return m, runOp("move", srcFS, paths, dstFS, dstDir, m.opCh)
	case "delete", "d", "D":
		srcs := m.marked()
		if len(srcs) == 0 && len(rows) > 0 && p.cursor < len(rows) {
			srcs = []Entry{rows[p.cursor]}
		}
		if len(srcs) == 0 {
			return m, nil
		}
		m.confirm = m.confirm[:0]
		for _, s := range srcs {
			m.confirm = append(m.confirm, s.Path)
		}
		if len(m.confirm) == 1 {
			m.message = "삭제 확인: " + m.confirm[0] + "  (y=실행)"
		} else {
			m.message = fmt.Sprintf("삭제 확인: %d개  (y=실행)", len(m.confirm))
		}
		return m, nil
	}
	m.clamp()
	return m, nil
}

func skipSuffix(n int64) string {
	if n > 0 {
		return fmt.Sprintf(" (같은 위치 %d개 건너뜀)", n)
	}
	return ""
}

// baseName은 경로의 마지막 요소를 반환한다.
func baseName(p string) string {
	if i := lastSep(p); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

// progressText는 상태바에 표시할 진행 문구를 만든다.
func progressText(s opState) string {
	label := "붙여넣기"
	if s.kind == "move" {
		label = "이동"
	}
	out := fmt.Sprintf("%s %d/%d", label, s.item, s.total)
	if s.current != "" {
		out += " · " + s.current
	}
	if s.files > 0 {
		out += fmt.Sprintf(" · 파일 %d개 · %s", s.files, human.Size(s.byts))
	}
	return out
}

// runOp는 복사/이동을 고루틴에서 실행하고 진행 상황을 ch로 흘려보낸다.
// 첫 진행 메시지를 기다리는 tea.Cmd를 반환한다.
func runOp(kind string, srcFS FS, paths []string, dstFS FS, dstDir string, ch chan opMsg) tea.Cmd {
	go func() {
		defer close(ch)
		var items, files, byts, skipped int64
		var firstErr error
		for i, src := range paths {
			ch <- opMsg{state: opState{kind: kind, item: i + 1, total: len(paths), files: files, byts: byts, current: baseName(src)}}
			if sameLocation(srcFS, src, dstFS, dstDir) {
				skipped++
				continue
			}
			prog := func(cf, cb int64) {
				ch <- opMsg{state: opState{kind: kind, item: i + 1, total: len(paths), files: files + cf, byts: byts + cb, current: baseName(src)}}
			}
			var f, b int64
			var err error
			if kind == "move" {
				f, b, err = Move(srcFS, src, dstFS, dstDir, prog)
			} else {
				f, b, err = Copy(srcFS, src, dstFS, dstDir, prog)
			}
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			items++
			files += f
			byts += b
		}
		label := "붙여넣기 완료"
		if kind == "move" {
			label = "이동 완료"
		}
		ch <- opMsg{done: true, desc: fmt.Sprintf("%s: %d개%s", label, items, skipSuffix(skipped)), err: firstErr}
	}()
	return readOp(ch)
}

// readOp는 진행 채널에서 메시지 하나를 읽는 tea.Cmd다.
func readOp(ch chan opMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// sameLocation은 붙여넣기/이동 목적지가 원본과 같은지 판별한다.
func sameLocation(srcFS FS, src string, dstFS FS, dstDir string) bool {
	_, srcLocal := srcFS.(LocalFS)
	_, dstLocal := dstFS.(LocalFS)
	if srcLocal != dstLocal {
		return false
	}
	base := src
	if i := lastSep(src); i >= 0 {
		base = src[i+1:]
	}
	var dest string
	if srcLocal {
		dest = filepath.Join(dstDir, base)
	} else {
		dest = path.Join(dstDir, base)
	}
	if srcLocal {
		a, err1 := filepath.Abs(src)
		b, err2 := filepath.Abs(dest)
		return err1 == nil && err2 == nil && a == b
	}
	return src == dest
}

func lastSep(s string) int {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return i
	}
	return strings.LastIndex(s, string(filepath.Separator))
}

// openConn은 SSH 접속 폼을 연다 (호스트/아이디/비밀번호).
// 최근 접속 기록이 있으면 호스트·아이디를 미리 채운다.
func (m Model) openConn() (tea.Model, tea.Cmd) {
	m.connMode = true
	m.connField = 0
	m.connPass = ""
	m.connHost, m.connUser = "", os.Getenv("USER")
	if len(m.addrHist) > 0 {
		if a, err := ParseAddr(m.addrHist[0]); err == nil {
			h := a.Host
			if a.Port != 22 {
				h = fmt.Sprintf("%s:%d", a.Host, a.Port)
			}
			m.connHost, m.connUser = h, a.User
		}
	}
	return m, nil
}

// updateConn은 접속 폼 입력을 처리한다.
func (m Model) updateConn(k string) (tea.Model, tea.Cmd) {
	field := func() *string {
		switch m.connField {
		case 1:
			return &m.connUser
		case 2:
			return &m.connPass
		default:
			return &m.connHost
		}
	}
	switch k {
	case "esc":
		m.connMode = false
		return m, nil
	case "tab", "down":
		if m.connField < 2 {
			m.connField++
		}
		return m, nil
	case "shift+tab", "up":
		if m.connField > 0 {
			m.connField--
		}
		return m, nil
	case "enter":
		if m.connField < 2 {
			m.connField++
			return m, nil
		}
		return m.connectFromForm()
	case "backspace":
		s := field()
		if r := []rune(*s); len(r) > 0 {
			*s = string(r[:len(r)-1])
		}
		return m, nil
	default:
		if len(k) == 1 {
			*field() += k
		}
		return m, nil
	}
}

// connPane은 접속 대상 패널(오른쪽 고정)이다. 왼쪽은 로컬, 오른쪽은 원격 용도.
const connPane = 1

// connectFromForm은 폼 입력으로 로컬 전환 또는 SSH 접속을 수행한다.
// 접속은 활성 패널과 무관하게 항상 오른쪽 패널에서 이뤄진다.
func (m Model) connectFromForm() (tea.Model, tea.Cmd) {
	host := strings.TrimSpace(m.connHost)
	user := strings.TrimSpace(m.connUser)
	pass := m.connPass
	if host == "" {
		m.message = "호스트(IP)를 입력하세요"
		return m, nil
	}
	// 아이디/비밀번호가 비어 있고 경로 형태면 로컬 경로 전환
	if user == "" && pass == "" &&
		(strings.HasPrefix(host, "/") || strings.HasPrefix(host, "~") || strings.HasPrefix(host, ".")) {
		m.connMode = false
		p := m.pane(connPane)
		_ = p.fs.Close()
		p.fs, p.remote = LocalFS{}, false
		p.label, p.path = host, host
		p.history = []string{host}
		m.message = "로컬 경로: " + host
		return m, listCmd(&m, connPane)
	}
	raw := host
	if user != "" {
		raw = user + "@" + host
	}
	addr, err := ParseAddr(raw)
	if err != nil {
		m.message = "주소 오류: " + err.Error()
		return m, nil
	}
	addr.Raw = raw
	m.connMode = false
	m.working = "접속 중… " + addr.String()
	pane := connPane
	return m, func() tea.Msg {
		fs, err := Dial(addr, pass)
		if err != nil {
			return connectedMsg{pane: pane, addr: addr, err: err}
		}
		return connectedMsg{pane: pane, fs: fs, addr: addr}
	}
}

// disconnect(b)는 활성 패널의 접속/연결을 해제하고 직전 상태로 되돌린다.
// 활성 패널에 백업이 없으면 다른 패널(주로 오른쪽 SSH)의 백업을 되돌린다.
func (m Model) disconnect() (tea.Model, tea.Cmd) {
	q := m.active
	if m.backup[q] == nil {
		if other := 1 - m.active; m.backup[other] != nil {
			q = other
		} else {
			m.message = "되돌릴 접속/연결이 없음"
			return m, nil
		}
	}
	p := m.pane(q)
	wasRemote := p.remote
	_ = p.fs.Close()
	*p = *m.backup[q]
	m.backup[q] = nil
	p.ready = false
	if wasRemote {
		m.message = "접속 종료"
	} else {
		m.message = "원래대로 복원"
	}
	return m, listCmd(&m, q)
}

// openVolumes는 외장디스크 메뉴를 연다.
func (m Model) openVolumes() (tea.Model, tea.Cmd) {
	list := volumes.ListExternal()
	if len(list) == 0 {
		m.message = "외장디스크 없음 (/Volumes 확인)"
		return m, nil
	}
	m.volMode = true
	m.volList = list
	m.volCursor = 0
	return m, nil
}

// updateVol은 외장디스크 메뉴 입력을 처리한다.
func (m Model) updateVol(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q":
		m.volMode = false
		return m, nil
	case "up", "k":
		if m.volCursor > 0 {
			m.volCursor--
		}
		return m, nil
	case "down", "j":
		if m.volCursor < len(m.volList)-1 {
			m.volCursor++
		}
		return m, nil
	case "enter", "right", "l":
		if m.volCursor < 0 || m.volCursor >= len(m.volList) {
			return m, nil
		}
		target := m.volList[m.volCursor]
		m.volMode = false
		p := m.pane(connPane)
		if p.remote {
			// 원격 패널을 외장디스크로 대체 → 닫힌 연결은 복원 불가하므로 백업 제거
			m.backup[connPane] = nil
		} else {
			// 연결 직전 상태 백업 (b 로 원복)
			b := *p
			m.backup[connPane] = &b
		}
		_ = p.fs.Close()
		p.fs, p.remote = LocalFS{}, false
		p.label, p.path = target, target
		p.history = []string{target}
		p.ready = false
		m.message = "외장디스크: " + target + " (b 원복)"
		return m, listCmd(&m, connPane)
	}
	return m, nil
}

// volumeView는 외장디스크 선택 메뉴(모달)를 그린다.
func (m Model) volumeView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("외장 디스크 선택") + "\n")
	b.WriteString(dimStyle.Render("Enter 열기(오른쪽) · ↑↓ 이동 · esc 취소") + "\n\n")
	for i, v := range m.volList {
		line := "  " + v
		if i == m.volCursor {
			line = activeStyle.Render("▶ " + v)
		}
		b.WriteString(line + "\n")
	}
	box := headerBox.Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func parentOf(p *Pane) string {
	if p.remote {
		d := path.Dir(p.path)
		if d == "." {
			return "/"
		}
		return d
	}
	return filepath.Dir(p.path)
}

func (m Model) deleteCmd(p *Pane, targets []string) tea.Cmd {
	fs := p.fs
	remote := p.remote
	return func() tea.Msg {
		if !remote {
			n, err := trash.MoveToTrash(targets)
			if err != nil {
				return doneOpMsg{err: err}
			}
			return doneOpMsg{desc: fmt.Sprintf("휴지통으로 이동: %d개", n)}
		}
		for _, t := range targets {
			if err := fs.RemoveAll(t); err != nil {
				return doneOpMsg{err: err}
			}
		}
		return doneOpMsg{desc: fmt.Sprintf("원격 삭제: %d개", len(targets))}
	}
}

func (m *Model) clamp() {
	lh := m.listH()
	for i, p := range []*Pane{&m.left, &m.right} {
		n := len(m.vis(p, i))
		if p.cursor >= n {
			p.cursor = n - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
		if p.cursor < p.offset {
			p.offset = p.cursor
		}
		if p.cursor >= p.offset+lh {
			p.offset = p.cursor - lh + 1
		}
	}
}

func (m Model) boxWidth() int {
	w := m.width - 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m Model) contentW() int {
	w := m.boxWidth() - 4
	if w < 10 {
		w = 10
	}
	return w
}

func (m Model) hintsLines() int {
	n := (runewidth.StringWidth(bottomHints(m)) + m.contentW() - 1) / m.contentW()
	if n < 1 {
		n = 1
	}
	return n
}

// listH는 박스를 제외한 목록 줄 수다. 전체 높이를 고정한다.
func (m Model) listH() int {
	// 헤더박스(테두리2+내용1) + 패널박스(제목2+테두리2) + 푸터박스(테두리2 + 상태1 + 복사대기줄 + 힌트줄)
	chrome := 3 + 4 + 2 + 1 + m.yankLines() + m.hintsLines()
	h := m.height - chrome
	if h < 5 {
		h = 5
	}
	return h
}

// yankLines은 상태바의 복사 대기 줄 수(0 또는 1)다.
func (m Model) yankLines() int {
	if len(m.yankPaths) > 0 {
		return 1
	}
	return 0
}

// yankLine은 복사 대기 중인 항목 목록을 한 줄로 만든다.
func (m Model) yankLine() string {
	names := make([]string, 0, len(m.yankPaths))
	for _, p := range m.yankPaths {
		names = append(names, baseName(p))
	}
	return checkStyle.Render(fmt.Sprintf("복사 대기 %d개: %s", len(names), strings.Join(names, ", ")))
}

func bottomHints(m Model) string {
	p := m.pane(m.active)
	dot := "숨김"
	if p.showDot {
		dot = "표시중"
	}
	sel := "선택꺼짐"
	if p.selMode {
		sel = "선택중"
	}
	return fmt.Sprintf("Tab 전환 · ↑↓ 이동 · Enter 진입 · ← 상위 · a 숨김파일(%s) · v 체크박스(%s) · Space 표시 · y 복사 · p 붙이기 · m 이동 · Del 삭제 · s 접속 · b 접속해제·원복 · e 외장디스크 · q 종료", dot, sel)
}

// connForm은 접속 폼(호스트/아이디/비밀번호) 한 줄을 만든다.
func (m Model) connForm() string {
	fld := func(i int, label, val string) string {
		s := label + ":" + val
		if m.connField == i {
			return activeStyle.Render(s + "█")
		}
		return dimStyle.Render(s)
	}
	mask := strings.Repeat("•", len([]rune(m.connPass)))
	line := titleStyle.Render("SSH 접속") + "  " +
		fld(0, "IP/호스트", m.connHost) + "  " +
		fld(1, "아이디", m.connUser) + "  " +
		fld(2, "비밀번호", mask) + "  " +
		dimStyle.Render("(Tab 이동 · Enter 연결 · esc 취소)")
	return headerBox.Width(m.boxWidth()).Render(line)
}

func (m Model) View() string {
	if m.volMode {
		return m.volumeView()
	}
	var b strings.Builder
	// 상단 주소줄 (고정 1줄)
	if m.connMode {
		b.WriteString(m.connForm() + "\n")
	} else {
		b.WriteString(headerBox.Width(m.boxWidth()).Render(
			titleStyle.Render("fcommand")+"  "+dimStyle.Render(m.addrLine())) + "\n")
	}
	pw := (m.contentW() - 1) / 2
	lh := m.listH()
	left := m.renderPaneBox(&m.left, 0, pw, lh)
	right := m.renderPaneBox(&m.right, 1, pw, lh)
	ll := strings.Split(left, "\n")
	rl := strings.Split(right, "\n")
	for i := 0; i < len(ll) && i < len(rl); i++ {
		b.WriteString(ll[i] + " " + rl[i] + "\n")
	}
	// 하단 상태바: 상태/진행 + 복사 대기 목록 + 힌트
	footer := []string{padCell(m.statusLine(), m.contentW())}
	if len(m.yankPaths) > 0 {
		footer = append(footer, padW(m.yankLine(), m.contentW()))
	}
	footer = append(footer, dimStyle.Render(wrapPad(bottomHints(m), m.contentW(), m.hintsLines())))
	b.WriteString(footerBox.Width(m.boxWidth()).Render(strings.Join(footer, "\n")))
	return b.String()
}

func (m Model) addrLine() string {
	p := m.pane(m.active)
	side := "좌"
	if m.active == 1 {
		side = "우"
	}
	yank := ""
	if len(m.yankPaths) > 0 {
		yank = fmt.Sprintf("  |  복사대기 %d개", len(m.yankPaths))
	}
	return fmt.Sprintf("[%s] %s%s  |  s 접속(기록 %d개)", side, p.label, yank, len(m.addrHist))
}

func (m Model) statusLine() string {
	if m.working != "" {
		return m.working
	}
	if m.message != "" {
		return m.message
	}
	return "준비"
}

// renderPaneBox는 고정 높이 박스 하나를 그린다 (제목 2줄 + 목록 lh줄).
func (m Model) renderPaneBox(p *Pane, idx, pw, lh int) string {
	var b strings.Builder
	title := p.label
	if p.remote {
		title = "ssh " + title
	}
	if m.active == idx {
		b.WriteString(activeStyle.Render("▶ "+shorten(title, pw-2)) + "\n")
	} else {
		b.WriteString("  " + shorten(title, pw-2) + "\n")
	}
	b.WriteString(dimStyle.Render(shorten(p.path, pw)) + "\n")
	n := 0
	write := func(s string) {
		if n < lh {
			b.WriteString(padW(s, pw) + "\n")
			n++
		}
	}
	rows := m.vis(p, idx)
	if !p.ready {
		write("읽는 중…")
	} else if p.errMsg != "" {
		write(errStyle.Render("오류: " + shorten(p.errMsg, pw-4)))
	} else if len(rows) == 0 {
		write(dimStyle.Render("(비어 있음)"))
	} else {
		checkW := 0
		if p.selMode {
			checkW = 4
		}
		// 오른쪽 블록(사이즈+날짜)은 너비 고정, 이름이 남는 칸을 쓴다
		const rightW = 25 // 8(사이즈) + " " + 16(날짜 "01-02 15:04")
		nameW := pw - (1 + 1 + checkW + 2) - rightW
		if nameW < 8 {
			nameW = 8
		}
		end := p.offset + lh
		if end > len(rows) {
			end = len(rows)
		}
		for i := p.offset; i < end; i++ {
			e := rows[i]
			check := ""
			if p.selMode {
				if m.marks[markKey(idx, e.Path)] {
					check = checkStyle.Render("[x] ")
				} else {
					check = "[ ] "
				}
			}
			size := "-"
			if !e.IsDir {
				size = human.Size(e.Size)
			}
			icon := "📁"
			if !e.IsDir {
				icon = "📄"
			}
			namestyle := lipgloss.NewStyle()
			if m.isYanked(idx, e.Path) {
				namestyle = yankStyle
			}
			line := fmt.Sprintf("%s %s%s%s%s %s",
				" ", check, icon, namestyle.Render(padCell(e.Name, nameW)), padCell(size, 8), e.ModTime.Format("01-02 15:04"))
			if runewidth.StringWidth(stripANSI(line)) > pw {
				line = runewidth.Truncate(stripANSI(line), pw, "…")
			}
			if i == p.cursor && m.active == idx {
				line = selStyle.Render("▶" + line)
			} else if i == p.cursor {
				line = activeStyle.Render("▶" + line)
			}
			write(line)
		}
	}
	for n < lh {
		b.WriteString(padW("", pw) + "\n")
		n++
	}
	return paneBox.Width(pw + 2).Render(strings.TrimSuffix(b.String(), "\n"))
}

func padW(s string, w int) string {
	sw := runewidth.StringWidth(stripANSI(s))
	if sw > w {
		return runewidth.Truncate(stripANSI(s), w-1, "…")
	}
	if strings.Contains(s, "\x1b[") {
		return s + strings.Repeat(" ", w-sw)
	}
	return s + strings.Repeat(" ", w-sw)
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		if r == 0x1b {
			esc = true
			continue
		}
		if esc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				esc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func shorten(s string, w int) string {
	if w < 4 {
		w = 4
	}
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w-1, "…")
	}
	return s
}

func wrapPad(s string, w, lines int) string {
	wrapped := strings.Split(runewidth.Wrap(s, w), "\n")
	if len(wrapped) > lines {
		wrapped = wrapped[:lines]
	}
	for len(wrapped) < lines {
		wrapped = append(wrapped, "")
	}
	return strings.Join(wrapped, "\n")
}

func padCell(s string, w int) string {
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w-1, "…")
	}
	return s + strings.Repeat(" ", w-runewidth.StringWidth(s))
}
