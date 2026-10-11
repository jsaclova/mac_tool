package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fm/internal/human"
	"fm/internal/scanner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	magentaStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	selStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	barRed       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	barYellow    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	barBlue      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	barGreen     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

type Row struct {
	Path  string
	Size  int64
	IsDir bool
}

type doneMsg struct {
	res    scanner.Result
	total  int64
	errors int64
	gen    int
}

type scanMsg struct {
	ch  <-chan scanner.Event
	ev  scanner.Event
	gen int
}

func waitScan(ch <-chan scanner.Event, g int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return scanMsg{ch, ev, g}
	}
}

type tickMsg struct{}

type Model struct {
	root      string
	dirs      []Row
	cursor    int
	offset    int
	width     int
	height    int
	termH     int
	done      bool
	frame     int
	total     int64
	free      int64
	diskTotal int64
	status    string
	history   []historyEntry
	visited   map[string]historyEntry
	gen       int
	scanCh    <-chan scanner.Event
	progFiles int64
	progBytes int64
	progStart time.Time
}

// historyEntry는 상위 복귀용 스냅샷이다. 재스캔 없이 즉시 복원한다.
type historyEntry struct {
	root   string
	dirs   []Row
	total  int64
	free   int64
	disk   int64
	status string
	cursor int
	offset int
}

func New(root string) Model {
	return Model{root: root, status: "스캔 중…", width: 120, height: 20, progStart: time.Now(), visited: map[string]historyEntry{}}
}

func (m Model) Init() tea.Cmd {
	root := m.root
	ch := scanner.ScanStream(root, 300, 0, nil)
	return tea.Batch(waitScan(ch, 0), tick())
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// rowsFromDirs는 직속 자식(서브폴더 + 바로 밑 파일) 목록을 만든다.
func rowsFromDirs(res scanner.Result) []Row {
	rows := make([]Row, 0, len(res.DirSizes))
	for _, d := range res.DirSizes {
		isDir := false
		if st, err := os.Stat(d.Path); err == nil {
			isDir = st.IsDir()
		}
		rows = append(rows, Row{d.Path, d.Size, isDir})
	}
	return rows
}

func barFor(pct float64, width int) string {
	if width < 1 {
		return ""
	}
	filled := int(pct / 100 * float64(width))
	if filled == 0 && pct > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled)
	switch {
	case pct > 50:
		return barRed.Render(bar)
	case pct > 20:
		return barYellow.Render(bar)
	case pct > 5:
		return barBlue.Render(bar)
	default:
		return barGreen.Render(bar)
	}
}

func diskInfo(path string) (total, free int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize)
}

func (m *Model) rescanCmd() tea.Cmd {
	m.gen++
	g := m.gen
	root := m.root
	m.progFiles, m.progBytes = 0, 0
	m.progStart = time.Now()
	ch := scanner.ScanStream(root, 300, 0, nil)
	m.scanCh = ch
	return tea.Batch(waitScan(ch, g), tick())
}

func (m *Model) applyDone(msg doneMsg) {
	m.done = true
	m.dirs = rowsFromDirs(msg.res)
	m.total = msg.total
	m.diskTotal, m.free = diskInfo(m.root)
	m.status = fmt.Sprintf("파일 %s개 · 실제점유 %s", comma(msg.res.TotalFiles), human.Size(msg.total))
	if msg.errors > 0 {
		m.status += fmt.Sprintf(" · 접근실패 %d (권한)", msg.errors)
	}
	if len(m.dirs) == 0 {
		m.status += " · 빈 폴더 또는 접근 불가"
	}
	m.cursor, m.offset = 0, 0
}

func (m Model) snapshot() historyEntry {
	return historyEntry{
		root: m.root, dirs: m.dirs,
		total: m.total, free: m.free, disk: m.diskTotal, status: m.status,
		cursor: m.cursor, offset: m.offset,
	}
}

func (m *Model) restore(h historyEntry) {
	m.root, m.dirs = h.root, h.dirs
	m.total, m.free, m.diskTotal, m.status = h.total, h.free, h.disk, h.status
	m.cursor, m.offset = h.cursor, h.offset
	m.done = true
	m.gen++ // 진행 중 스캔 결과 무효화
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.termH = msg.Height
	case tickMsg:
		m.frame++
		if !m.done {
			return m, tick()
		}
	case scanMsg:
		if msg.gen != m.gen {
			return m, nil // 낡은 스캔 결과 버림
		}
		m.scanCh = msg.ch
		if !msg.ev.Done {
			m.progFiles, m.progBytes = msg.ev.Files, msg.ev.Bytes
			return m, waitScan(msg.ch, msg.gen)
		}
		m.scanCh = nil
		res := msg.ev.Result
		m.applyDone(doneMsg{res, res.TotalBytes, res.Errors, msg.gen})
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.dirs)-1 {
				m.cursor++
			}
		case "enter", "right", "l":
			if len(m.dirs) > 0 && m.cursor < len(m.dirs) && m.dirs[m.cursor].IsDir {
				if st, err := os.Stat(m.dirs[m.cursor].Path); err == nil && st.IsDir() {
					snap := m.snapshot()
					m.history = append(m.history, snap)
					m.visited[m.root] = snap
					target := m.dirs[m.cursor].Path
					if h, ok := m.visited[target]; ok {
						m.restore(h) // 방문 캐시에서 즉시 복원
						return m, nil
					}
					m.root = target
					m.dirs = nil
					m.cursor, m.offset = 0, 0
					m.done = false
					m.status = "스캔 중…"
					cmd := m.rescanCmd()
					return m, cmd
				}
			}
		case "backspace", "left", "h":
			if n := len(m.history); n > 0 {
				if m.done {
					m.visited[m.root] = m.snapshot()
				}
				h := m.history[n-1]
				m.history = m.history[:n-1]
				m.restore(h)
				return m, nil
			}
			parent := filepath.Dir(m.root)
			if parent != m.root {
				m.root = parent
				m.dirs = nil
				m.cursor, m.offset = 0, 0
				m.done = false
				m.status = "스캔 중…"
				cmd := m.rescanCmd()
				return m, cmd
			}
		}
	}
	if m.cursor >= len(m.dirs) {
		m.cursor = len(m.dirs) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if lh := m.listHeight(); m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
	return m, nil
}

func (m Model) listHeight() int {
	termH := m.termH
	if termH <= 0 {
		termH = m.height + 12
	}
	chrome := 3 + 2 + 2 + 1 + m.hintsLines() // 헤더박스 + 목록박스 테두리 + 푸터박스 테두리 + 상태줄 + 힌트줄
	lh := termH - chrome
	if lh < 3 {
		lh = 3
	}
	return lh
}

// contentW는 박스 안 실제 텍스트 너비다.
func (m Model) contentW() int {
	w := m.boxWidth() - 4
	if w < 10 {
		w = 10
	}
	return w
}

func (m Model) hintsLines() int {
	n := (runewidth.StringWidth(bottomHints()) + m.contentW() - 1) / m.contentW()
	if n < 1 {
		n = 1
	}
	return n
}

// wrapPad는 텍스트를 너비에 맞춰 감고 정확히 lines줄로 맞춘다.
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

// headLine은 항상 1줄인 헤더 텍스트를 만든다. 경로는 뒤쪽만 살린다.
func (m Model) headLine() string {
	folder := "  |  폴더: " + human.Size(m.total)
	disk := ""
	if m.diskTotal > 0 {
		disk = fmt.Sprintf("  |  디스크: %s/%s (%s free)",
			human.Size(m.diskTotal-m.free), human.Size(m.diskTotal), human.Size(m.free))
	}
	const title = "Analyze Disk  "
	avail := m.contentW() - runewidth.StringWidth(title+folder+disk)
	root := m.root
	if runewidth.StringWidth(root) > avail {
		root = "…" + stringTail(root, avail-1)
	}
	return magentaStyle.Render("Analyze Disk") + dimStyle.Render("  "+root+folder+disk)
}

func stringTail(s string, w int) string {
	if w <= 0 {
		return ""
	}
	runes := []rune(s)
	width := 0
	i := len(runes)
	for i > 0 {
		wc := runewidth.RuneWidth(runes[i-1])
		if width+wc > w {
			break
		}
		width += wc
		i--
	}
	return string(runes[i:])
}

var (
	headerBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("13")).Padding(0, 1)
	footerBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	listBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
)

func (m Model) boxWidth() int {
	w := m.width - 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(headerBox.Width(m.boxWidth()).Render(m.headLine()) + "\n")
	b.WriteString(m.listView() + "\n")
	b.WriteString(footerBox.Width(m.boxWidth()).Render(
		padCell(m.status, m.contentW()) + "\n" + dimStyle.Render(wrapPad(bottomHints(), m.contentW(), m.hintsLines()))))
	return b.String()
}

// listView는 항상 listHeight줄을 채운 목록 박스다.
func (m Model) listView() string {
	lh := m.listHeight()
	var lb strings.Builder
	n := 0
	write := func(s string) {
		if n < lh {
			lb.WriteString(s + "\n")
			n++
		}
	}
	if !m.done {
		sp := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}[m.frame%10]
		el := time.Since(m.progStart).Round(time.Second)
		rate := ""
		if secs := time.Since(m.progStart).Seconds(); secs > 0.5 && m.progFiles > 0 {
			rate = fmt.Sprintf(" · %s 파일/초", comma(int64(float64(m.progFiles)/secs)))
		}
		write(fmt.Sprintf("%s 스캔 중… %s 파일 · %s · %v 경과%s",
			sp, comma(m.progFiles), human.Size(m.progBytes), el, rate))
	} else {
		nameW := 32
		barW := m.boxWidth() - nameW - 44
		if barW > 46 {
			barW = 46
		}
		if barW < 8 {
			barW = 8
		}
		if len(m.dirs) == 0 {
			write(dimStyle.Render("(비어 있음)"))
		}
		end := m.offset + lh
		if end > len(m.dirs) {
			end = len(m.dirs)
		}
		for i := m.offset; i < end; i++ {
			r := m.dirs[i]
			pct := 0.0
			if m.total > 0 {
				pct = float64(r.Size) / float64(m.total) * 100
			}
			cursor := "  "
			name := r.Path[strings.LastIndex(r.Path, "/")+1:]
			icon := "📁"
			if !r.IsDir {
				icon = "📄"
			}
			namestyle := lipgloss.NewStyle()
			if i == m.cursor {
				cursor = "▶"
				namestyle = selStyle
			}
			pctStr := fmt.Sprintf("%6.1f%%", pct)
			if pct < 0.1 && r.Size > 0 {
				pctStr = "  <0.1%"
			}
			write(fmt.Sprintf("%s ○ %3d. %s %s │ %s %s %10s",
				cursor, i+1, barFor(pct, barW), pctStr,
				icon, namestyle.Render(padCell(name, nameW)), human.Size(r.Size)))
		}
	}
	for n < lh {
		lb.WriteString("\n")
		n++
	}
	return listBox.Width(m.boxWidth()).Render(strings.TrimSuffix(lb.String(), "\n"))
}

// comma는 천 단위 콤마를 찍는다.
func comma(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// padCell은 셀 너비 기준 잘라내기/채우기로 열을 정렬한다.
func padCell(s string, w int) string {
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w-1, "…")
	}
	return s + strings.Repeat(" ", w-runewidth.StringWidth(s))
}

func bottomHints() string {
	return "↑↓ / j/k 이동 · Enter 하위폴더 · ←/h 상위폴더 · q 종료"
}
