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
	// BuildTag는 실행 버전 식별용이다 (? 도움말과 fcommand -h에 표시).
	BuildTag    = "2026-10-13f"
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("15"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	checkStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	yankStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	helpTitle   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("14"))
	headerBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("13")).Padding(0, 1)
	helpBox     = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("13")).Padding(0, 1)
	paneBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
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
	kind      string // "paste" | "move"
	item      int    // 현재 항목 (1-based)
	total     int    // 전체 항목 수
	files     int64  // 지금까지 처리한 파일 수
	byts      int64  // 지금까지 처리한 바이트
	totalByts int64  // 현재 파일 전체 바이트 (파일별 진행률용, 모르면 -1)
	fileDone  int64  // 현재 파일 처리 바이트
	current   string // 현재 항목 이름
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
	gotoMode    bool
	gotoInput   string
	helpMode    bool
	working     string
	message     string
	opHist      []string // 최근 완료 작업 기록 (최대 2개, 우측 프로세스칸 표시용)
	modal       *modal   // 확인 팝업 (삭제/덮어쓰기), nil이면 없음
}

// modal은 확인 팝업이다 (삭제 확인, 덮어쓰기 확인).
type modal struct {
	kind     string // "delete" | "overwrite"
	title    string
	lines    []string // 항목 목록 (표시용)
	question string
	targets  []string // delete: 삭제 대상 경로
	remote   bool     // delete: 원격 즉시 삭제 안내
	opKind   string   // overwrite: "paste" | "move"
	srcFS    FS
	paths    []string
	dstFS    FS
	dstDir   string
	skip     map[string]bool // overwrite: 건너뛸 원본 경로 집합
}

func New(leftPath, rightPath string) Model {
	m := Model{width: 160, height: 30, addrHist: LoadHosts(), marks: map[string]bool{}}
	m.left = Pane{fs: LocalFS{}, label: leftPath, path: leftPath}
	m.right = Pane{fs: LocalFS{}, label: rightPath, path: rightPath}
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
			m.pushHist(m.message)
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
		m.pushHist(m.message)
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
	if m.helpMode {
		if k == "esc" || k == "q" || k == "?" || k == "/" || k == "enter" {
			m.helpMode = false
		}
		return m, nil
	}
	if m.gotoMode {
		return m.updateGoto(k)
	}
	if m.modal != nil {
		return m.updateModal(k)
	}
	p := m.pane(m.active)
	rows := m.vis(p, m.active)
	switch k {
	case "q":
		_ = m.left.fs.Close()
		_ = m.right.fs.Close()
		return m, tea.Quit
	case "/":
		m.gotoMode = true
		m.gotoInput = "/"
		return m, nil
	case "?":
		m.helpMode = true
		return m, nil
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
		// ←는 항상 상위 폴더로 이동한다 (연타해도 한 칸씩 꾸준히 상승).
		// Enter로 들어온 곳이면 히스토리도 함께 소모한다.
		up := parentOf(p)
		if up != p.path {
			if n := len(p.history); n > 0 && p.history[n-1] == up {
				p.history = p.history[:n-1]
			} else {
				p.history = append(p.history, p.path)
			}
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
		if coll := collideBases(dst.entries, yankFS, paths, dstFS, dstDir); len(coll) > 0 {
			m.modal = overwriteModal("paste", yankFS, paths, dstFS, dstDir, coll)
			return m, nil
		}
		return m.startOp("paste", yankFS, paths, dstFS, dstDir)
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
		if coll := collideBases(dst.entries, srcFS, paths, dstFS, dstDir); len(coll) > 0 {
			m.modal = overwriteModal("move", srcFS, paths, dstFS, dstDir, coll)
			return m, nil
		}
		return m.startOp("move", srcFS, paths, dstFS, dstDir)
	case "delete", "d", "D":
		srcs := m.marked()
		if len(srcs) == 0 && len(rows) > 0 && p.cursor < len(rows) {
			srcs = []Entry{rows[p.cursor]}
		}
		if len(srcs) == 0 {
			return m, nil
		}
		targets := make([]string, 0, len(srcs))
		for _, s := range srcs {
			targets = append(targets, s.Path)
		}
		m.modal = deleteModal(p.remote, targets)
		return m, nil
	}
	m.clamp()
	return m, nil
}

// startOp는 복사/이동 작업을 시작한다 (진행 채널+상태 표시 후 runOp).
func (m Model) startOp(kind string, srcFS FS, paths []string, dstFS FS, dstDir string) (tea.Model, tea.Cmd) {
	m.opCh = make(chan opMsg, 128)
	label := "붙여넣기"
	if kind == "move" {
		label = "이동"
	}
	m.working = fmt.Sprintf("%s... 0/%d -> %s", label, len(paths), dstDir)
	return m, runOp(kind, srcFS, paths, dstFS, dstDir, m.opCh)
}

// collideBases는 목적지에 동명 항목이 있어 덮어쓰게 되는 원본 경로를 모은다.
// 원본과 동일한 위치(sameLocation, runOp에서 건너뜀)는 제외한다.
// dstEntries는 최신 목록 그대로(숨김 포함) 쓴다.
func collideBases(dstEntries []Entry, srcFS FS, paths []string, dstFS FS, dstDir string) []string {
	names := make(map[string]bool, len(dstEntries))
	for _, e := range dstEntries {
		names[e.Name] = true
	}
	var out []string
	for _, src := range paths {
		if sameLocation(srcFS, src, dstFS, dstDir) {
			continue
		}
		if names[baseName(src)] {
			out = append(out, src)
		}
	}
	return out
}

// deleteModal은 "삭제하겠습니까?" 팝업을 만든다.
func deleteModal(remote bool, targets []string) *modal {
	lines := make([]string, 0, len(targets)+1)
	for i, t := range targets {
		if i >= 6 {
			lines = append(lines, fmt.Sprintf("외 %d개", len(targets)-6))
			break
		}
		lines = append(lines, t)
	}
	q := "삭제하겠습니까? (y=실행, n=취소)"
	if remote {
		q = "삭제하겠습니까? (원격 즉시 삭제, y=실행, n=취소)"
	}
	return &modal{kind: "delete", title: "삭제 확인", lines: lines, question: q, targets: targets, remote: remote}
}

// overwriteModal은 "덮어쓰겠습니까?" 팝업을 만든다.
func overwriteModal(opKind string, srcFS FS, paths []string, dstFS FS, dstDir string, coll []string) *modal {
	lines := []string{"받을 곳: " + dstDir}
	for i, c := range coll {
		if i >= 6 {
			lines = append(lines, fmt.Sprintf("외 %d개 겹침", len(coll)-6))
			break
		}
		lines = append(lines, baseName(c)+" (있음)")
	}
	skip := make(map[string]bool, len(coll))
	for _, c := range coll {
		skip[c] = true
	}
	return &modal{kind: "overwrite", title: "덮어쓰기 확인", lines: lines,
		question: "덮어쓰겠습니까? (y=덮어쓰기, s=건너뛰기, n=취소)",
		opKind:   opKind, srcFS: srcFS, paths: paths, dstFS: dstFS, dstDir: dstDir, skip: skip}
}

// updateModal은 확인 팝업의 키 입력을 처리한다.
func (m Model) updateModal(k string) (tea.Model, tea.Cmd) {
	md := m.modal
	switch md.kind {
	case "delete":
		switch k {
		case "y", "Y", "enter", "d", "D", "delete":
			m.modal = nil
			p := m.pane(m.active)
			return m, m.deleteCmd(p, md.targets)
		case "n", "N", "esc", "q":
			m.modal = nil
			m.message = "취소됨"
			return m, nil
		}
		return m, nil
	case "overwrite":
		switch k {
		case "y", "Y": // 모두 덮어쓰기
			m.modal = nil
			return m.startOp(md.opKind, md.srcFS, md.paths, md.dstFS, md.dstDir)
		case "s", "S": // 충돌 항목만 건너뛰고 계속
			kept := make([]string, 0, len(md.paths))
			for _, p := range md.paths {
				if !md.skip[p] {
					kept = append(kept, p)
				}
			}
			m.modal = nil
			if len(kept) == 0 {
				m.message = "모두 건너뜀"
				return m, nil
			}
			return m.startOp(md.opKind, md.srcFS, kept, md.dstFS, md.dstDir)
		case "n", "N", "esc", "q":
			m.modal = nil
			m.message = "취소됨"
			return m, nil
		}
		return m, nil
	}
	m.modal = nil
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

// progressText는 상태바에 표시할 진행 문구를 만든다 (파일별 진행률+바 포함).
func progressText(s opState) string {
	label := "붙여넣기"
	if s.kind == "move" {
		label = "이동"
	}
	out := fmt.Sprintf("%s %d/%d", label, s.item, s.total)
	if pct, ok := progressPct(s); ok {
		out += fmt.Sprintf(" | %d%% %s", pct, barStr(pct, 10))
	}
	if s.current != "" {
		out += " | " + s.current
	}
	if s.files > 0 {
		out += fmt.Sprintf(" | 파일 %d개 | %s", s.files, human.Size(s.byts))
	}
	return out
}

// progressPct는 현재 파일의 진행률을 구한다. 파일 크기를 모르면 false.
func progressPct(s opState) (int, bool) {
	if s.totalByts > 0 && s.fileDone >= 0 {
		p := int(s.fileDone * 100 / s.totalByts)
		if p > 100 {
			p = 100
		}
		if p < 0 {
			p = 0
		}
		return p, true
	}
	return 0, false
}

// barStr는 ASCII 진행 바를 만든다 ([======>   ], 폭 정확).
func barStr(pct, w int) string {
	if w < 3 {
		w = 3
	}
	f := pct * w / 100
	if f > w {
		f = w
	}
	if f >= w {
		return "[" + strings.Repeat("=", w) + "]"
	}
	return "[" + strings.Repeat("=", f) + ">" + strings.Repeat(" ", w-1-f) + "]"
}

// pushHist는 완료된 작업 문구를 최근 기록(최대 2개) 앞에 넣는다.
func (m *Model) pushHist(s string) {
	if s == "" {
		return
	}
	if len(m.opHist) > 0 && m.opHist[0] == s {
		return
	}
	m.opHist = append([]string{s}, m.opHist...)
	if len(m.opHist) > 2 {
		m.opHist = m.opHist[:2]
	}
}

// runOp는 복사/이동을 고루틴에서 실행하고 진행 상황을 ch로 흘려보낸다.
// 첫 진행 메시지를 기다리는 tea.Cmd를 반환한다.
func runOp(kind string, srcFS FS, paths []string, dstFS FS, dstDir string, ch chan opMsg) tea.Cmd {
	go func() {
		defer close(ch)
		var items, files, byts, skipped int64
		var firstErr error
		// 단일 파일이면 전체 바이트를 미리 구해 둔다 (여러 파일은 각 파일 시작 시 앎).
		totalB := int64(-1)
		if len(paths) == 1 {
			totalB = SizeOf(srcFS, paths[0])
		}
		for i, src := range paths {
			ch <- opMsg{state: opState{kind: kind, item: i + 1, total: len(paths), files: files, byts: byts, totalByts: totalB, fileDone: 0, current: baseName(src)}}
			if sameLocation(srcFS, src, dstFS, dstDir) {
				skipped++
				continue
			}
			prog := func(fp FileProg) {
				ch <- opMsg{state: opState{kind: kind, item: i + 1, total: len(paths), files: files + fp.Files, byts: byts + fp.Bytes, totalByts: fp.FileTotal, fileDone: fp.FileDone, current: fp.Name}}
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
	m.working = "접속 중... " + addr.String()
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

// modalView는 확인 팝업(삭제/덮어쓰기)을 중앙에 그린다.
func (m Model) modalView() string {
	md := m.modal
	maxW := m.width - 8
	if maxW < 40 {
		maxW = 40
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(md.title) + "\n")
	for _, l := range md.lines {
		b.WriteString(shorten(l, maxW) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render(md.question))
	box := headerBox.Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
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

// listH는 박스를 제외한 목록 줄 수다. 전체 높이를 터미널에 정확히 맞춘다
// (줄 수 초과 시 맨 윗줄이 스크롤로 잘리므로 newlines = height-1이 되게).
// 헤더 3 + 패널(제목1+테두리2) 3 + 푸터(테두리2+내용4) 6 = 12 크롬.
func (m Model) listH() int {
	h := m.height - 12
	if h < 5 {
		h = 5
	}
	return h
}

// paneWidths는 좌우 박스 내용폭이다. 좌박스를 조금 줄이고,
// 좌(+테두리2) + 구분자1 + 우(+테두리2) = 터미널폭에 정확히 맞춘다.
func (m Model) paneWidths() (left, right int) {
	sum := m.boxWidth() - 3
	left = sum/2 - 2
	if left < 10 {
		left = 10
	}
	right = sum - left
	if right < 10 {
		right = 10
	}
	return left, right
}

// clipboardLine은 복사 대상(yank) 고정 1줄이다.
func (m Model) clipboardLine() string {
	if len(m.yankPaths) == 0 {
		return dimStyle.Render("Clipboard: (비어 있음)")
	}
	names := make([]string, 0, len(m.yankPaths))
	for _, p := range m.yankPaths {
		names = append(names, baseName(p))
	}
	return checkStyle.Render(fmt.Sprintf("Clipboard: 복사 대기 %d개: %s", len(names), strings.Join(names, ", ")))
}

// countLine은 좌우 항목 수와 선택 수를 보여주는 1줄이다.
func (m Model) countLine() string {
	l := len(m.vis(&m.left, 0))
	r := len(m.vis(&m.right, 1))
	return fmt.Sprintf("좌 %d개 | 우 %d개 | 선택 %d개", l, r, len(m.marks))
}

// processLine은 복사/이동 진행상태 1줄이다.
func (m Model) processLine() string {
	if m.working != "" {
		return "Process: " + m.working
	}
	if m.message != "" {
		return "Process: " + m.message
	}
	return dimStyle.Render("Process: 대기 중")
}

// clipboardRows는 좌측 클립보드칸 4줄이다 (요약 1줄 + 이름 최대 3줄).
func (m Model) clipboardRows(w int) []string {
	rows := make([]string, 0, 4)
	if len(m.yankPaths) == 0 {
		rows = append(rows, dimStyle.Render("Clipboard: (비어 있음)"))
	} else {
		rows = append(rows, checkStyle.Render(fmt.Sprintf("Clipboard: 복사 대기 %d개", len(m.yankPaths))))
		names := make([]string, 0, len(m.yankPaths))
		for _, p := range m.yankPaths {
			names = append(names, baseName(p))
		}
		joined := strings.ReplaceAll(strings.Join(names, ", "), "\n", " ")
		for _, wl := range strings.Split(runewidth.Wrap(joined, w), "\n") {
			if len(rows) >= 4 {
				break
			}
			rows = append(rows, checkStyle.Render(wl))
		}
	}
	for len(rows) < 4 {
		rows = append(rows, "")
	}
	return rows
}

// processRows는 우측 프로세스칸 4줄이다 (상태 1줄 + 최근 기록 2줄 + 항목수 1줄).
func (m Model) processRows(w int) []string {
	_ = w
	rows := []string{m.processLine()}
	if len(m.opHist) > 0 {
		rows = append(rows, "최근: "+m.opHist[0])
	} else {
		rows = append(rows, dimStyle.Render("(완료 기록 없음)"))
	}
	if len(m.opHist) > 1 {
		rows = append(rows, "이전: "+m.opHist[1])
	} else {
		rows = append(rows, "")
	}
	return append(rows, m.countLine())
}

// updateGoto는 / 이동 프롬프트 입력을 처리한다.
// "/?"·"/h"(또는 "?"·"h")면 도움말, 그 외는 경로 이동. "/" 단독은 루트.
func (m Model) updateGoto(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc":
		m.gotoMode = false
		m.gotoInput = ""
		return m, nil
	case "enter":
		return m.commitGoto()
	case "backspace":
		if r := []rune(m.gotoInput); len(r) > 0 {
			m.gotoInput = string(r[:len(r)-1])
		}
		return m, nil
	default:
		if len(k) == 1 {
			m.gotoInput += k
		}
		return m, nil
	}
}

// commitGoto는 gotoInput을 확정한다.
func (m Model) commitGoto() (tea.Model, tea.Cmd) {
	in := strings.TrimSpace(m.gotoInput)
	m.gotoMode = false
	m.gotoInput = ""
	// 도움말: /? /h ? h help
	trim := strings.TrimSpace(in)
	if trim == "?" || trim == "h" || trim == "/?" || trim == "/h" ||
		trim == "help" || trim == "/help" {
		m.helpMode = true
		return m, nil
	}
	if trim == "" {
		m.message = "취소됨"
		return m, nil
	}
	p := m.pane(m.active)
	target := expandGoto(p, trim)
	if target == "" {
		m.message = "경로 오류: " + in
		return m, nil
	}
	p.history = append(p.history, p.path)
	p.path = target
	p.ready = false
	return m, listCmd(&m, m.active)
}

// expandGoto는 입력 경로를 절대경로로 펼친다. "~" 홈, 상대경로는 현재 경로 기준.
func expandGoto(p *Pane, in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return ""
	}
	if s == "~" || strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if s == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(s, "~/"))
		}
	}
	if p.remote {
		if strings.HasPrefix(s, "/") {
			return path.Clean(s)
		}
		return path.Join(p.path, s)
	}
	if filepath.IsAbs(s) {
		return filepath.Clean(s)
	}
	return filepath.Join(p.path, s)
}

// gotoBar는 상단 이동 프롬프트 1줄이다.
func (m Model) gotoBar() string {
	line := titleStyle.Render("이동") + "  " +
		activeStyle.Render(m.gotoInput+"_") + "  " +
		dimStyle.Render("(Enter 이동 | /? 도움말 | esc 취소)")
	return headerBox.Width(m.boxWidth()).Render(line)
}

// helpLines는 /?·/h 도움말 본문 2줄이다.
func helpLines() [2]string {
	return [2]string{
		"Tab 전환 | ↑↓ 이동 | Enter 진입 | ←/h 상위 | / 이동 | ? 도움말 | q 종료",
		"a 숨김 | v 선택 | Space 표시 | y 복사 | p 붙이기 | m 이동 | d 삭제 | s 접속 | b 해제 | e 외장",
	}
}

// helpView는 키 설명 모달을 그린다 (두줄 테두리, 본문 2줄, 강조 제목).
func (m Model) helpView() string {
	hl := helpLines()
	body := hl[0] + "\n" + hl[1]
	if m.width < 100 {
		// 좁은 터미널에서는 자동 줄바꿈 (모달 깨짐 방지)
		body = runewidth.Wrap(hl[0]+" | "+hl[1], m.width-8)
	}
	var b strings.Builder
	b.WriteString(helpTitle.Render("도움말  (/? | /h)") + "\n")
	b.WriteString(body + "\n\n")
	b.WriteString(dimStyle.Render("/ 이동: 경로+Enter (예: / | /tmp | ~/work)") + "\n")
	b.WriteString(dimStyle.Render("Enter/esc 닫기 | 빌드 " + BuildTag))
	box := helpBox.Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// connForm은 접속 폼(호스트/아이디/비밀번호) 한 줄을 만든다.
func (m Model) connForm() string {
	fld := func(i int, label, val string) string {
		s := label + ":" + val
		if m.connField == i {
			return activeStyle.Render(s + "_")
		}
		return dimStyle.Render(s)
	}
	mask := strings.Repeat("•", len([]rune(m.connPass)))
	line := titleStyle.Render("SSH 접속") + "  " +
		fld(0, "IP/호스트", m.connHost) + "  " +
		fld(1, "아이디", m.connUser) + "  " +
		fld(2, "비밀번호", mask) + "  " +
		dimStyle.Render("(Tab 이동 | Enter 연결 | esc 취소)")
	return headerBox.Width(m.boxWidth()).Render(line)
}

func (m Model) View() string {
	if m.modal != nil {
		return m.modalView()
	}
	if m.volMode {
		return m.volumeView()
	}
	if m.helpMode {
		return m.helpView()
	}
	var b strings.Builder
	// 상단 주소줄 (고정 1줄)
	if m.connMode {
		b.WriteString(m.connForm() + "\n")
	} else if m.gotoMode {
		b.WriteString(m.gotoBar() + "\n")
	} else {
		b.WriteString(headerBox.Width(m.boxWidth()).Render(
			titleStyle.Render("fcommand")+"  "+dimStyle.Render(m.addrLine())) + "\n")
	}
	pwL, pwR := m.paneWidths()
	lh := m.listH()
	left := m.renderPaneBox(&m.left, 0, pwL, lh)
	right := m.renderPaneBox(&m.right, 1, pwR, lh)
	ll := strings.Split(left, "\n")
	rl := strings.Split(right, "\n")
	for i := 0; i < len(ll) && i < len(rl); i++ {
		b.WriteString(ll[i] + " " + rl[i] + "\n")
	}
	// 하단 분리 박스: 좌측 클립보드 + 우측 프로세스 (위 네비게이션 박스와 동일 형식)
	// 끝줄 개행 없음 (전체 줄 수 = 터미널 높이 유지)
	bl, br := m.paneWidths()
	lb := renderBottomBox(m.clipboardRows(bl-2), bl)
	rb := renderBottomBox(m.processRows(br-2), br)
	fll := strings.Split(lb, "\n")
	frl := strings.Split(rb, "\n")
	for i := 0; i < len(fll) && i < len(frl); i++ {
		b.WriteString(fll[i] + " " + frl[i])
		if i+1 < len(fll) && i+1 < len(frl) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderBottomBox는 하단 분리 박스 하나를 그린다 (내용 4줄 고정).
func renderBottomBox(rows []string, boxW int) string {
	tw := boxW - 2
	out := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		s := ""
		if i < len(rows) {
			s = rows[i]
		}
		out = append(out, padW(s, tw))
	}
	return paneBox.Width(boxW).Render(strings.Join(out, "\n"))
}

func (m Model) addrLine() string {
	p := m.pane(m.active)
	side := "좌"
	if m.active == 1 {
		side = "우"
	}
	// 현재 위치만 표시 (이동 시 갱신). "fcommand  " 접두사까지 포함해
	// 헤더 1줄 고정을 위해 줄인다 (넘치면 박스가 감겨 전체 높이가 틀어짐).
	loc := p.path
	if p.remote {
		loc = "ssh " + p.label + " " + p.path
	}
	hint := "  |  /? 도움말"
	avail := m.boxWidth() - runewidth.StringWidth("fcommand  ["+side+"]"+hint) - 1
	if avail < 6 {
		avail = 6
	}
	return fmt.Sprintf("[%s] %s%s", side, shorten(loc, avail), hint)
}

// renderPaneBox는 고정 높이 박스 하나를 그린다 (제목 1줄 + 목록 lh줄).
// boxW는 박스 내용폭, 실제 텍스트폭은 tw=boxW-2(좌우 패딩)다.
func (m Model) renderPaneBox(p *Pane, idx, boxW, lh int) string {
	tw := boxW - 2
	var b strings.Builder
	title := p.path
	if p.remote {
		title = "ssh " + p.label + " " + p.path
	}
	if m.active == idx {
		b.WriteString(activeStyle.Render("> "+shorten(title, tw-2)) + "\n")
	} else {
		b.WriteString("  " + shorten(title, tw-2) + "\n")
	}
	n := 0
	write := func(s string) {
		if n < lh {
			b.WriteString(padW(s, tw) + "\n")
			n++
		}
	}
	rows := m.vis(p, idx)
	if !p.ready {
		write("읽는 중...")
	} else if p.errMsg != "" {
		write(errStyle.Render("오류: " + shorten(p.errMsg, tw-4)))
	} else if len(rows) == 0 {
		write(dimStyle.Render("(비어 있음)"))
	} else {
		checkW := 0
		if p.selMode {
			checkW = 4
		}
		// 오른쪽 블록(사이즈+날짜)은 너비 고정, 이름이 남는 칸을 쓴다
		const rightW = 25 // 8(사이즈) + " " + 16(날짜 "01-02 15:04")
		nameW := tw - (1 + 1 + checkW + 2) - rightW
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
			plain := disp(stripANSI(line))
			if runewidth.StringWidth(plain) > tw {
				line = runewidth.Truncate(plain, tw, "...")
			}
			if i == p.cursor && m.active == idx {
				line = selStyle.Render(">" + line)
			} else if i == p.cursor {
				line = activeStyle.Render(">" + line)
			}
			write(line)
		}
	}
	for n < lh {
		b.WriteString(padW("", tw) + "\n")
		n++
	}
	return paneBox.Width(boxW).Render(strings.TrimSuffix(b.String(), "\n"))
}

func padW(s string, w int) string {
	s = disp(s)
	sw := runewidth.StringWidth(stripANSI(s))
	if sw > w {
		return runewidth.Truncate(stripANSI(s), w-3, "...")
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

// dispReplacer는 터미널마다 셀 폭이 달라지는 Ambiguous 문자들을
// 항상 1셀인 ASCII로 바꾼다 (ko 로케일에서 –·…▶→ 등이 2셀로 측정돼
// 박스를 넘어가는 문제를 막는다. ESC 시퀀스에는 영향 없음).
var dispReplacer = strings.NewReplacer(
	"–", "-", "—", "-", "‒", "-", "―", "-",
	"·", "-", "…", "...", "•", "*",
	"▶", ">", "◀", "<",
	"→", "->", "←", "<-", "↑", "^", "↓", "v",
	"│", "|", "─", "-", "═", "=", "║", "|",
	"╭", "+", "╮", "+", "╰", "+", "╯", "+",
	"█", "#", "“", "\"", "”", "\"", "‘", "'", "’", "'",
	"×", "x", "÷", "/",
	"\t", " ", "\n", " ", "\r", " ",
)

// disp는 폭 측정 대상 문자열을 터미널 독립적으로 정규화한다.
// 탭·개행도 공백으로 바꿔 행 분리/탭 점프를 막는다.
func disp(s string) string {
	if strings.ContainsAny(s, "–—‒―·…•▶◀→←↑↓│─═║╭╮╰╯█“”‘’×÷\t\n\r") {
		return dispReplacer.Replace(s)
	}
	return s
}

func shorten(s string, w int) string {
	s = disp(s)
	if w < 6 {
		w = 6
	}
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w-3, "...")
	}
	return s
}

func padCell(s string, w int) string {
	s = disp(s)
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w-3, "...")
	}
	return s + strings.Repeat(" ", w-runewidth.StringWidth(s))
}
