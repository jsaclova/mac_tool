package dualpane

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"fm/internal/human"
	"fm/internal/trash"
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
)

// Pane은 좌/우 한 패널이다.
type Pane struct {
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

type connectedMsg struct {
	pane   int
	fs     FS
	addr   Addr
	err    error
	needPW bool
}

type Model struct {
	left, right Pane
	active      int
	width       int
	height      int
	addrMode    bool
	addrInput   string
	addrHist    []string
	histIdx     int
	pwMode      bool
	pwInput     string
	pendingAddr Addr
	working     string
	confirm     string
	message     string
}

func paneFS() FS { return LocalFS{} }

func New(leftPath, rightPath string) Model {
	m := Model{width: 160, height: 30, addrHist: LoadHosts()}
	m.left = Pane{fs: paneFS(), label: leftPath, path: leftPath, history: []string{leftPath}}
	m.right = Pane{fs: paneFS(), label: rightPath, path: rightPath, history: []string{rightPath}}
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
	case doneOpMsg:
		m.working = ""
		if msg.err != nil {
			m.message = "실패: " + msg.err.Error()
		} else {
			m.message = msg.desc
		}
		return m, tea.Batch(listCmd(&m, 0), listCmd(&m, 1))
	case connectedMsg:
		m.working = ""
		if msg.needPW {
			m.pwMode = true
			m.pwInput = ""
			m.pendingAddr = msg.addr
			m.message = "비밀번호 입력 (Enter 연결, esc 취소)"
			return m, nil
		}
		if msg.err != nil {
			m.message = "접속 실패: " + msg.err.Error()
			return m, nil
		}
		p := m.pane(msg.pane)
		_ = p.fs.Close()
		p.fs = msg.fs
		p.remote = true
		p.label = msg.addr.String()
		p.path = msg.addr.Path
		p.history = []string{p.path}
		SaveHost(msg.addr.Raw)
		m.addrHist = LoadHosts()
		m.message = "접속됨: " + msg.addr.String()
		return m, listCmd(&m, msg.pane)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	// 주소 입력 모드 (상단 주소창)
	if m.addrMode {
		switch k {
		case "esc":
			m.addrMode = false
			m.addrInput = ""
			return m, nil
		case "enter":
			raw := strings.TrimSpace(m.addrInput)
			m.addrMode = false
			m.addrInput = ""
			if raw == "" {
				return m, nil
			}
			if !strings.Contains(raw, "@") && !strings.HasPrefix(raw, "ssh://") && !strings.Contains(raw, ":") {
				// 로컬 경로로 전환
				p := m.pane(m.active)
				_ = p.fs.Close()
				p.fs, p.remote = LocalFS{}, false
				p.label, p.path = raw, raw
				p.history = []string{raw}
				return m, listCmd(&m, m.active)
			}
			addr, err := ParseAddr(raw)
			if err != nil {
				m.message = "주소 오류: " + err.Error()
				return m, nil
			}
			m.working = "접속 중… " + addr.String()
			pane := m.active
			return m, func() tea.Msg {
				fs, err := Dial(addr, "")
				if err != nil && strings.Contains(err.Error(), "인증") {
					return connectedMsg{pane: pane, addr: addr, needPW: true}
				}
				if err != nil {
					// 키 실패 시 비밀번호 입력으로 유도
					if strings.Contains(err.Error(), "unable to authenticate") || strings.Contains(err.Error(), "handshake") {
						return connectedMsg{pane: pane, addr: addr, needPW: true}
					}
					return connectedMsg{pane: pane, addr: addr, err: err}
				}
				return connectedMsg{pane: pane, fs: fs, addr: addr}
			}
		case "backspace":
			if len(m.addrInput) > 0 {
				m.addrInput = m.addrInput[:len(m.addrInput)-1]
			}
			return m, nil
		case "up":
			if len(m.addrHist) > 0 {
				m.histIdx = (m.histIdx + 1) % len(m.addrHist)
				m.addrInput = m.addrHist[m.histIdx]
			}
			return m, nil
		case "down":
			if len(m.addrHist) > 0 {
				m.histIdx = (m.histIdx - 1 + len(m.addrHist)) % len(m.addrHist)
				m.addrInput = m.addrHist[m.histIdx]
			}
			return m, nil
		default:
			if len(k) == 1 {
				m.addrInput += k
			}
			return m, nil
		}
	}
	// 비밀번호 입력 모드
	if m.pwMode {
		switch k {
		case "esc":
			m.pwMode = false
			return m, nil
		case "enter":
			pw := m.pwInput
			addr := m.pendingAddr
			pane := m.active
			m.pwMode = false
			m.pwInput = ""
			m.working = "접속 중… " + addr.String()
			return m, func() tea.Msg {
				fs, err := Dial(addr, pw)
				if err != nil {
					return connectedMsg{pane: pane, addr: addr, err: err}
				}
				return connectedMsg{pane: pane, fs: fs, addr: addr}
			}
		case "backspace":
			if len(m.pwInput) > 0 {
				m.pwInput = m.pwInput[:len(m.pwInput)-1]
			}
			return m, nil
		default:
			if len(k) == 1 {
				m.pwInput += k
			}
			return m, nil
		}
	}
	// 삭제 확인 대기
	if m.confirm != "" {
		if k == "y" || k == "Y" {
			target := m.confirm
			m.confirm = ""
			p := m.pane(m.active)
			return m, m.deleteCmd(p, target)
		}
		m.confirm = ""
		m.message = "취소됨"
		return m, nil
	}
	p := m.pane(m.active)
	rows := len(p.entries)
	switch k {
	case "q", "ctrl+c":
		_ = m.left.fs.Close()
		_ = m.right.fs.Close()
		return m, tea.Quit
	case "tab":
		m.active = 1 - m.active
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < rows-1 {
			p.cursor++
		}
	case "enter", "right", "l":
		if rows > 0 && p.cursor < rows && p.entries[p.cursor].IsDir {
			e := p.entries[p.cursor]
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
	case "s":
		m.addrMode = true
		m.addrInput = ""
		m.histIdx = -1
		return m, nil
	case "c":
		return m.copyOrMove(false, p)
	case "m":
		return m.copyOrMove(true, p)
	case "delete", "d", "D":
		if rows > 0 && p.cursor < rows {
			m.confirm = p.entries[p.cursor].Path
			m.message = "삭제 확인: " + m.confirm + "  (y=실행, 그외=취소)"
		}
		return m, nil
	}
	m.clamp()
	return m, nil
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

func (m Model) copyOrMove(move bool, p *Pane) (tea.Model, tea.Cmd) {
	if len(p.entries) == 0 || p.cursor >= len(p.entries) {
		m.message = "선택 항목 없음"
		return m, nil
	}
	src := p.entries[p.cursor]
	dst := m.pane(1 - m.active)
	verb := "복사"
	if move {
		verb = "이동"
	}
	m.working = fmt.Sprintf("%s 중… %s → %s", verb, src.Name, dst.path)
	return m, func() tea.Msg {
		var files, bytes int64
		var err error
		if move {
			files, bytes, err = Move(p.fs, src.Path, dst.fs, dst.path, nil)
		} else {
			files, bytes, err = Copy(p.fs, src.Path, dst.fs, dst.path, nil)
		}
		if err != nil {
			return doneOpMsg{err: err}
		}
		if move {
			return doneOpMsg{desc: fmt.Sprintf("이동 완료: %s", src.Name)}
		}
		return doneOpMsg{desc: fmt.Sprintf("복사 완료: %s (%d 파일, %s)", src.Name, files, human.Size(bytes))}
	}
}

func (m Model) deleteCmd(p *Pane, target string) tea.Cmd {
	fs := p.fs
	remote := p.remote
	return func() tea.Msg {
		var err error
		if remote {
			err = fs.RemoveAll(target)
		} else {
			_, err = trash.MoveToTrash([]string{target})
		}
		if err != nil {
			return doneOpMsg{err: err}
		}
		return doneOpMsg{desc: "삭제됨 (휴지통/원격): " + target}
	}
}

func (m *Model) clamp() {
	for _, p := range []*Pane{&m.left, &m.right} {
		if p.cursor >= len(p.entries) {
			p.cursor = len(p.entries) - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
		lh := m.listH()
		if p.cursor < p.offset {
			p.offset = p.cursor
		}
		if p.cursor >= p.offset+lh {
			p.offset = p.cursor - lh + 1
		}
	}
}

func (m Model) listH() int {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

func (m Model) View() string {
	var b strings.Builder
	pw := (m.width - 3) / 2
	if pw < 20 {
		pw = 20
	}
	// 상단 주소창 (활성 패널 기준)
	if m.addrMode {
		b.WriteString(titleStyle.Render("SSH/경로: ") + m.addrInput + "█  " + dimStyle.Render("(Enter 연결 · ↑↓ 기록 · esc 취소)") + "\n")
	} else if m.pwMode {
		b.WriteString(titleStyle.Render("비밀번호: ") + strings.Repeat("*", len(m.pwInput)) + "█\n")
	} else {
		b.WriteString(dimStyle.Render("[s] 주소연결 (SSH 캐시 "+fmt.Sprintf("%d", len(m.addrHist))+"개)") + "\n")
	}
	b.WriteString(m.renderPanes(pw) + "\n")
	if m.working != "" {
		b.WriteString(m.working + "\n")
	} else if m.message != "" {
		if strings.HasPrefix(m.message, "실패") || strings.HasPrefix(m.message, "접속 실패") || strings.HasPrefix(m.message, "주소 오류") {
			b.WriteString(errStyle.Render(m.message) + "\n")
		} else {
			b.WriteString(m.message + "\n")
		}
	} else {
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("Tab 전환 · ↑↓ 이동 · Enter 진입 · ← 상위 · c 복사→ · m 이동→ · Del 삭제 · s 주소 · q 종료"))
	return b.String()
}

func (m Model) renderPanes(pw int) string {
	lh := m.listH()
	left := m.renderPane(&m.left, m.active == 0, pw, lh)
	right := m.renderPane(&m.right, m.active == 1, pw, lh)
	ll := strings.Split(left, "\n")
	rl := strings.Split(right, "\n")
	var b strings.Builder
	for i := 0; i < len(ll) && i < len(rl); i++ {
		b.WriteString(ll[i] + " │ " + rl[i])
		if i+1 < len(ll) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) renderPane(p *Pane, active bool, pw, lh int) string {
	var b strings.Builder
	title := p.label
	if p.remote {
		title = "ssh " + title
	}
	if active {
		title = activeStyle.Render("▶ " + title)
	} else {
		title = "  " + title
	}
	b.WriteString(padW(title, pw) + "\n")
	b.WriteString(padW(dimStyle.Render(shorten(p.path, pw)), pw) + "\n")
	if !p.ready {
		b.WriteString(padW("읽는 중…", pw) + "\n")
		for i := 1; i < lh; i++ {
			b.WriteString(padW("", pw) + "\n")
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	if p.errMsg != "" {
		b.WriteString(padW(errStyle.Render("오류: "+shorten(p.errMsg, pw-4)), pw) + "\n")
		for i := 1; i < lh; i++ {
			b.WriteString(padW("", pw) + "\n")
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	end := p.offset + lh
	if end > len(p.entries) {
		end = len(p.entries)
	}
	n := 0
	for i := p.offset; i < end; i++ {
		e := p.entries[i]
		size := human.Size(e.Size)
		if e.IsDir {
			size = "DIR"
		}
		name := shorten(e.Name, pw-22)
		if name == "" {
			name = e.Name
		}
		line := fmt.Sprintf("%s %s %8s %s", " ", name, size, e.ModTime.Format("01-02 15:04"))
		if i == p.cursor && active {
			line = selStyle.Render(line)
		} else if i == p.cursor {
			line = activeStyle.Render("▶" + line[1:])
		}
		n++
		b.WriteString(padW(line, pw) + "\n")
	}
	for ; n < lh; n++ {
		b.WriteString(padW("", pw) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
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
