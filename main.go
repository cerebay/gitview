// gv (git view) — 轻量 Git 面板（终端界面）
//
// 用法: gv [仓库路径]
// 按键: j/k ↑/↓ 移动 · Enter 打开/切换/展开 · d 删除分支 · p 发布 · r 刷新 · q 退出
// 鼠标: 点击选中，点击标题折叠，滚轮滚动
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

const (
	commitLimit  = 200
	refreshEvery = 3 * time.Second
)

var repo string

// ---------- git ----------

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return strings.TrimRight(string(out), "\n"), fmt.Errorf("%s", errLine(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func errLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for _, l := range lines {
		ll := strings.ToLower(l)
		if strings.HasPrefix(ll, "fatal") || strings.HasPrefix(ll, "error") {
			return l
		}
	}
	return lines[0]
}

type commit struct {
	hash, short, subject string
	ts                   int64
}

type branch struct {
	name    string
	current bool
}

type gitData struct {
	head          string
	detached      bool
	upstream      string
	ahead, behind int
	changes       int
	commits       []commit
	branches      []branch
	at            time.Time
}

func loadData() gitData {
	d := gitData{at: time.Now()}
	d.head, _ = git("rev-parse", "--abbrev-ref", "HEAD")
	if d.head == "" {
		d.head = "?"
	}
	d.detached = d.head == "HEAD"
	if !d.detached {
		if up, err := git("rev-parse", "--abbrev-ref", "@{u}"); err == nil {
			d.upstream = up
			if s, err := git("rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
				f := strings.Fields(s)
				if len(f) == 2 {
					d.ahead, _ = strconv.Atoi(f[0])
					d.behind, _ = strconv.Atoi(f[1])
				}
			}
		}
	}
	if s, _ := git("status", "--porcelain"); s != "" {
		d.changes = len(strings.Split(s, "\n"))
	}
	if s, _ := git("log", fmt.Sprintf("-n%d", commitLimit), "--format=%H%x1f%h%x1f%s%x1f%ct"); s != "" {
		for _, l := range strings.Split(s, "\n") {
			p := strings.Split(l, "\x1f")
			if len(p) == 4 {
				ts, _ := strconv.ParseInt(p[3], 10, 64)
				d.commits = append(d.commits, commit{p[0], p[1], p[2], ts})
			}
		}
	}
	if s, _ := git("for-each-ref", "refs/heads", "--sort=-committerdate", "--format=%(refname:short)%1f%(HEAD)"); s != "" {
		for _, l := range strings.Split(s, "\n") {
			p := strings.Split(l, "\x1f")
			if len(p) == 2 {
				d.branches = append(d.branches, branch{p[0], p[1] == "*"})
			}
		}
	}
	return d
}

func ago(ts int64) string {
	s := time.Now().Unix() - ts
	switch {
	case s < 60:
		return "刚刚"
	case s < 3600:
		return fmt.Sprintf("%d 分钟前", s/60)
	case s < 86400:
		return fmt.Sprintf("%d 小时前", s/3600)
	case s < 86400*30:
		return fmt.Sprintf("%d 天前", s/86400)
	case s < 86400*365:
		return fmt.Sprintf("%d 个月前", s/86400/30)
	}
	return fmt.Sprintf("%d 年前", s/86400/365)
}

// ---------- 样式 ----------

const visibleCommits = 6 // 提交列表默认显示条数，更多的往下滚

var (
	cText   = lipgloss.Color("#f8f8f2")
	cAccent = lipgloss.Color("#bd93f9")
	cGreen  = lipgloss.Color("#50fa7b")
	cRed    = lipgloss.Color("#ff5555")
	cOrange = lipgloss.Color("#ffb86c")
	cBlue   = lipgloss.Color("#8be9fd")
	cTeal   = lipgloss.Color("#5eead4")
	cYellow = lipgloss.Color("#f1fa8c")
	cPink   = lipgloss.Color("#ff79c6")
	cGray   = lipgloss.Color("#7b88b8")
	cDark   = lipgloss.Color("#1e1f29")
	cSelBg  = lipgloss.Color("#343746")
	cKeyBg  = lipgloss.Color("#44475a")

	sText   = lipgloss.NewStyle().Foreground(cText)
	sBold   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sGray   = lipgloss.NewStyle().Foreground(cGray)
	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sGreen  = lipgloss.NewStyle().Foreground(cGreen)
	sRed    = lipgloss.NewStyle().Foreground(cRed)
	sOrange = lipgloss.NewStyle().Foreground(cOrange)
	sBlue   = lipgloss.NewStyle().Foreground(cBlue)
)

func pill(bg lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Background(bg).Foreground(cDark).Bold(true).Render(" " + text + " ")
}

var badgePalette = []lipgloss.Color{cBlue, cTeal, cYellow, cPink, cOrange, cAccent}

var typeColors = map[string]lipgloss.Color{
	"feat": cGreen, "fix": cRed, "docs": cBlue, "refactor": cAccent, "perf": cOrange,
	"test": cYellow, "chore": cGray, "ci": cGray, "build": cGray, "style": cPink, "revert": cPink,
}

// 把 "scope: message" 拆成前缀和正文，前缀按类型/名字着色
func splitSubject(s string) (prefix, rest string, color lipgloss.Color) {
	if i := strings.Index(s, ": "); i > 0 && i <= 24 && !strings.ContainsAny(s[:i], " “\"") {
		p := s[:i]
		base := strings.ToLower(p)
		if j := strings.IndexAny(base, "(!"); j > 0 {
			base = base[:j]
		}
		if c, ok := typeColors[base]; ok {
			return p, s[i+2:], c
		}
		sum := 0
		for _, b := range []byte(base) {
			sum += int(b)
		}
		return p, s[i+2:], badgePalette[sum%len(badgePalette)]
	}
	if strings.HasPrefix(s, "Revert") {
		return "Revert", strings.TrimSpace(strings.TrimPrefix(s, "Revert")), cPink
	}
	return "", s, cText
}

// 渐变文字
func gradient(text string, from, to [3]int) string {
	rs := []rune(text)
	var b strings.Builder
	for i, r := range rs {
		t := 0
		if len(rs) > 1 {
			t = i * 100 / (len(rs) - 1)
		}
		c := [3]int{}
		for k := 0; k < 3; k++ {
			c[k] = from[k] + (to[k]-from[k])*t/100
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]))).Render(string(r)))
	}
	return b.String()
}

type seg struct {
	t string
	s lipgloss.Style
}

func segW(ss []seg) int {
	w := 0
	for _, x := range ss {
		w += lipgloss.Width(x.t)
	}
	return w
}

// 渲染一行：左段 + 填充 + 右段。flex 是左段里可被截断的那一段的下标（-1 表示没有）
func line(w int, left []seg, flex int, right []seg, fill string, fillStyle lipgloss.Style, selected bool) string {
	if flex >= 0 {
		other := segW(left) - lipgloss.Width(left[flex].t)
		avail := w - other - segW(right) - 1
		left[flex].t = ansi.Truncate(left[flex].t, max(0, avail), "…")
	}
	gap := max(1, w-segW(left)-segW(right))
	all := append(append([]seg{}, left...), seg{strings.Repeat(fill, gap), fillStyle})
	all = append(all, right...)
	var b strings.Builder
	for _, x := range all {
		st := x.s
		if selected {
			st = st.Background(cSelBg)
		}
		b.WriteString(st.Render(x.t))
	}
	return b.String()
}

func justify(left, right string, w int) string {
	gap := max(1, w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func trunc(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// 滚动条：total 条里显示 [off, off+size)，返回 size 个字符
func scrollbar(total, size, off, i int) string {
	if total <= size {
		return " "
	}
	thumb := max(1, size*size/total)
	top := 0
	if total > size {
		top = off * (size - thumb) / (total - size)
	}
	if i >= top && i < top+thumb {
		return sAccent.Render("┃")
	}
	return sGray.Faint(true).Render("│")
}

// ---------- 模型 ----------

type mode int

const (
	modeList mode = iota
	modeDetail
	modeConfirm
)

type rowKind int

const (
	rowHeader rowKind = iota
	rowCommit
	rowBranch
)

type row struct {
	kind   rowKind
	key    string // header: commits / branches
	label  string
	count  int
	commit commit
	first  bool
	branch branch
}

type model struct {
	w, h      int
	data      gitData
	folded    map[string]bool
	cursor    int
	cOff      int // 提交窗口的滚动偏移
	bOff      int // 分支窗口的滚动偏移
	msg       string
	busy      bool
	mode      mode
	detail    []string
	detailTop int
	detailTtl string
	confirm   string
	onYes     func() tea.Cmd
}

type dataMsg gitData
type tickMsg struct{}
type doneMsg struct {
	msg string
	err error
}
type confirmForceMsg struct{ name string }
type detailMsg struct {
	title string
	lines []string
}

func (m model) Init() tea.Cmd { return tea.Batch(loadCmd, tick()) }

func loadCmd() tea.Msg { return dataMsg(loadData()) }
func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func gitCmd(okMsg string, args ...string) tea.Cmd {
	return func() tea.Msg {
		_, err := git(args...)
		return doneMsg{okMsg, err}
	}
}

func (m model) rows() []row {
	r := []row{{kind: rowHeader, key: "commits", label: "提交", count: len(m.data.commits)}}
	if !m.folded["commits"] {
		for i, c := range m.data.commits {
			r = append(r, row{kind: rowCommit, commit: c, first: i == 0})
		}
	}
	r = append(r, row{kind: rowHeader, key: "branches", label: "分支", count: len(m.data.branches)})
	if !m.folded["branches"] {
		for _, b := range m.data.branches {
			r = append(r, row{kind: rowBranch, branch: b})
		}
	}
	return r
}

// 两个列表窗口各显示几行
const fixedLines = 13

func (m model) windows() (cw, bw int) {
	if !m.folded["commits"] {
		cw = min(visibleCommits, len(m.data.commits))
	}
	if !m.folded["branches"] {
		bw = len(m.data.branches)
	}
	over := cw + bw - (m.h - fixedLines)
	if over > 0 {
		cut := min(over, max(0, bw-2))
		bw -= cut
		over -= cut
	}
	if over > 0 {
		cw -= min(over, max(0, cw-2))
	}
	return
}

// 光标移动后，调整两个窗口的滚动偏移，保证光标可见
func (m model) fixScroll() model {
	cw, bw := m.windows()
	nc := len(m.data.commits)
	if m.folded["commits"] {
		nc = 0
	}
	if cur := m.cursor - 1; cur >= 0 && cur < nc {
		if cur < m.cOff {
			m.cOff = cur
		}
		if cur >= m.cOff+cw {
			m.cOff = cur - cw + 1
		}
	}
	if cur := m.cursor - nc - 2; cur >= 0 && cur < len(m.data.branches) {
		if cur < m.bOff {
			m.bOff = cur
		}
		if cur >= m.bOff+bw {
			m.bOff = cur - bw + 1
		}
	}
	m.cOff = max(0, min(m.cOff, max(0, nc-cw)))
	m.bOff = max(0, min(m.bOff, max(0, len(m.data.branches)-bw)))
	return m
}

// ---------- 更新 ----------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if nm, ok := next.(model); ok {
		if n := len(nm.rows()); nm.cursor >= n {
			nm.cursor = n - 1
		}
		next = nm.fixScroll()
	}
	return next, cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case dataMsg:
		m.data = gitData(msg)
	case tickMsg:
		if m.mode == modeList && !m.busy {
			return m, tea.Batch(loadCmd, tick())
		}
		return m, tick()
	case doneMsg:
		m.busy = false
		if msg.err != nil {
			m.msg = sRed.Render("✗ " + msg.err.Error())
		} else {
			m.msg = sGreen.Render("✓ " + msg.msg)
		}
		return m, loadCmd
	case confirmForceMsg:
		m.busy = false
		name := msg.name
		m.mode, m.confirm = modeConfirm, name+" 还没合并，强制删除？"
		m.onYes = func() tea.Cmd { return gitCmd("已强制删除 "+name, "branch", "-D", name) }
		return m, nil
	case detailMsg:
		m.mode, m.detail, m.detailTtl, m.detailTop = modeDetail, msg.lines, msg.title, 0
	case tea.MouseMsg:
		return m.onMouse(msg)
	case tea.KeyMsg:
		switch m.mode {
		case modeConfirm:
			return m.onConfirm(msg)
		case modeDetail:
			return m.onDetailKey(msg)
		}
		return m.onKey(msg)
	}
	return m, nil
}

func (m model) onConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	fn := m.onYes
	m.mode, m.onYes, m.msg = modeList, nil, ""
	if s := k.String(); s == "y" || s == "Y" {
		m.busy = true
		return m, fn()
	}
	return m, nil
}

func (m model) detailBody() int { return max(1, m.h-4) }

func (m model) onDetailKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	body := m.detailBody()
	limit := max(0, len(m.detail)-body)
	switch k.String() {
	case "q", "esc", "h", "left":
		m.mode = modeList
	case "j", "down":
		m.detailTop = min(limit, m.detailTop+1)
	case "k", "up":
		m.detailTop = max(0, m.detailTop-1)
	case " ", "pgdown":
		m.detailTop = min(limit, m.detailTop+body)
	case "pgup":
		m.detailTop = max(0, m.detailTop-body)
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) activate(r row) (model, tea.Cmd) {
	switch r.kind {
	case rowHeader:
		m.folded[r.key] = !m.folded[r.key]
	case rowCommit:
		c := r.commit
		return m, func() tea.Msg {
			out, err := git("show", "--stat", "--format=%an <%ae>%n%ad%n%n%B", "--date=format:%Y-%m-%d %H:%M", c.hash)
			if err != nil {
				out = "(无法读取)"
			}
			return detailMsg{c.short + "  " + c.subject, strings.Split(out, "\n")}
		}
	case rowBranch:
		if r.branch.current {
			return m, nil
		}
		if m.data.changes > 0 {
			m.msg = sRed.Render("✗ 有未提交的更改，已取消切换")
			return m, nil
		}
		m.busy = true
		return m, gitCmd("已切换到 "+r.branch.name, "switch", r.branch.name)
	}
	return m, nil
}

func (m model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	rs := m.rows()
	if m.cursor >= len(rs) {
		m.cursor = len(rs) - 1
	}
	cur := rs[m.cursor]
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = min(len(rs)-1, m.cursor+1)
	case "k", "up":
		m.cursor = max(0, m.cursor-1)
	case "tab", "shift+tab", "b", "c":
		// 在 提交 / 分支 两栏之间跳转
		nc := len(rs)
		for i, r := range rs {
			if r.kind == rowHeader && r.key == "branches" {
				nc = i
			}
		}
		toBranches := m.cursor < nc
		switch k.String() {
		case "b":
			toBranches = true
		case "c":
			toBranches = false
		}
		if toBranches {
			m.cursor = min(len(rs)-1, nc+1)
		} else {
			m.cursor = min(nc-1, 1)
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(rs) - 1
	case "r":
		m.msg = sGray.Render("已刷新")
		return m, loadCmd
	case "left", "h":
		if cur.kind == rowHeader {
			m.folded[cur.key] = true
		}
	case "enter", " ", "right", "l":
		return m.activate(cur)
	case "p":
		if m.data.detached {
			m.msg = sRed.Render("✗ 游离 HEAD 无法发布")
			break
		}
		m.busy = true
		if m.data.upstream == "" {
			return m, gitCmd("已发布到 origin", "push", "-u", "origin", m.data.head)
		}
		return m, gitCmd("已推送", "push")
	case "d":
		if cur.kind != rowBranch {
			break
		}
		if cur.branch.current {
			m.msg = sRed.Render("✗ 不能删除当前分支")
			break
		}
		name := cur.branch.name
		m.mode, m.confirm = modeConfirm, "删除分支 "+name+"？"
		m.onYes = func() tea.Cmd {
			return func() tea.Msg {
				_, err := git("branch", "-d", name)
				if err != nil && strings.Contains(err.Error(), "not fully merged") {
					return confirmForceMsg{name}
				}
				return doneMsg{"已删除 " + name, err}
			}
		}
	}
	return m, nil
}

func (m model) onMouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeDetail {
		limit := max(0, len(m.detail)-m.detailBody())
		switch e.Button {
		case tea.MouseButtonWheelDown:
			m.detailTop = min(limit, m.detailTop+3)
		case tea.MouseButtonWheelUp:
			m.detailTop = max(0, m.detailTop-3)
		}
		return m, nil
	}
	if m.mode != modeList {
		return m, nil
	}
	rs := m.rows()
	switch e.Button {
	case tea.MouseButtonWheelDown:
		m.cursor = min(len(rs)-1, m.cursor+2)
	case tea.MouseButtonWheelUp:
		m.cursor = max(0, m.cursor-2)
	case tea.MouseButtonLeft:
		if e.Action != tea.MouseActionPress {
			break
		}
		_, rowY := m.build()
		if idx, ok := rowY[e.Y]; ok {
			if m.cursor == idx || rs[idx].kind == rowHeader {
				m.cursor = idx
				return m.activate(rs[idx])
			}
			m.cursor = idx
		}
	}
	return m, nil
}

// ---------- 视图 ----------

// build 生成列表视图的所有行，并返回 屏幕行号 -> rows() 下标 的映射（供鼠标使用）
func (m model) build() ([]string, map[int]int) {
	w := m.w
	rowY := map[int]int{}
	var lines []string

	// 标题栏
	title := gradient("◆ git view", [3]int{189, 147, 249}, [3]int{255, 121, 198}) + "  " + sGray.Render(filepath.Base(repo))
	status := sGray.Render("↻ 刷新 r")
	if m.busy {
		status = sOrange.Render("◌ 处理中…")
	}
	lines = append(lines, justify(title, sGray.Render(m.data.at.Format("15:04")+" 更新")+"  "+status, w), "")

	// 分支卡片
	inner := w - 4
	name := m.data.head
	if m.data.detached {
		name = "游离 HEAD"
	}
	var tags []string
	switch {
	case !m.data.detached && m.data.upstream == "":
		tags = append(tags, pill(cOrange, "未发布"))
	case m.data.ahead > 0 || m.data.behind > 0:
		if m.data.ahead > 0 {
			tags = append(tags, pill(cBlue, fmt.Sprintf("↑%d", m.data.ahead)))
		}
		if m.data.behind > 0 {
			tags = append(tags, pill(cYellow, fmt.Sprintf("↓%d", m.data.behind)))
		}
	case !m.data.detached:
		tags = append(tags, pill(cGreen, "✓ 已同步"))
	}
	tag := strings.Join(tags, " ")
	branchPill := lipgloss.NewStyle().Background(cAccent).Foreground(cDark).Bold(true).
		Render(" ⎇ " + trunc(name, max(4, inner-lipgloss.Width(tag)-8)) + " ")
	up := sGray.Render("还没有上游分支")
	if m.data.upstream != "" {
		up = sGray.Render("上游 ") + sBlue.Render(m.data.upstream)
	}
	chg := sGreen.Render("● ") + sGray.Render("工作区干净")
	if m.data.changes > 0 {
		chg = sOrange.Render(fmt.Sprintf("● %d 个更改", m.data.changes))
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#6272a4")).Padding(0, 1).Width(w - 2)
	lines = append(lines, strings.Split(box.Render(justify(branchPill, tag, inner)+"\n"+trunc(up+sGray.Render("  ·  ")+chg, inner)), "\n")...)
	lines = append(lines, "")

	rs := m.rows()
	cw, bw := m.windows()
	nc := len(m.data.commits)
	if m.folded["commits"] {
		nc = 0
	}
	cursor := min(m.cursor, len(rs)-1)
	rule := sGray.Faint(true)

	header := func(i int) {
		r := rs[i]
		arrow := "▾"
		if m.folded[r.key] {
			arrow = "▸"
		}
		cnt := []seg{{" " + strconv.Itoa(r.count), sGray}}
		lines = append(lines, line(w, []seg{{arrow + " ", sAccent}, {r.label + " ", sBold}}, -1, cnt, "─", rule, i == cursor))
		rowY[len(lines)-1] = i
	}

	// 提交
	header(0)
	if !m.folded["commits"] {
		inW := w - 2 // 右边留一列给滚动条
		for k := 0; k < cw; k++ {
			idx := m.cOff + k
			if idx >= nc {
				break
			}
			ri := idx + 1
			c := rs[ri].commit
			sel := ri == cursor
			dot := seg{"○ ", sGray}
			if rs[ri].first {
				dot = seg{"● ", sGreen.Bold(true)}
			}
			prefix, rest, col := splitSubject(c.subject)
			left := []seg{{" ", sText}, dot}
			if prefix != "" {
				sep := ": "
				if prefix == "Revert" {
					sep = " "
				}
				left = append(left, seg{prefix + sep, lipgloss.NewStyle().Foreground(col).Bold(true)})
			}
			left = append(left, seg{rest, sText})
			right := []seg{{ago(c.ts), sGray}, {" ", sText}}
			if w >= 64 {
				right = append([]seg{{c.short + "  ", sGray.Faint(true)}}, right...)
			}
			l := line(inW, left, len(left)-1, right, " ", sText, sel)
			if sel {
				l = lipgloss.NewStyle().Foreground(cAccent).Background(cSelBg).Render("▌") + ansi.Cut(line(inW, left, len(left)-1, right, " ", sText, true), 1, inW)
			}
			rowY[len(lines)] = ri
			lines = append(lines, l+" "+scrollbar(nc, cw, m.cOff, k))
		}
	}
	lines = append(lines, "")

	// 分支
	header(nc + 1)
	if !m.folded["branches"] {
		inW := w - 2
		for k := 0; k < bw; k++ {
			j := m.bOff + k
			if j >= len(m.data.branches) {
				break
			}
			ri := nc + 2 + j
			b := m.data.branches[j]
			sel := ri == cursor
			icon, nameStyle := seg{"⎇ ", sGray}, sText
			right := []seg{{"删除 d ", sGray.Faint(true)}}
			if b.current {
				icon, nameStyle = seg{"⎇ ", sGreen}, sGreen.Bold(true)
				right = []seg{{"● 当前 ", sGreen}}
			}
			left := []seg{{" ", sText}, icon, {b.name, nameStyle}}
			l := line(inW, left, 2, right, " ", sText, sel)
			if sel {
				l = lipgloss.NewStyle().Foreground(cAccent).Background(cSelBg).Render("▌") + ansi.Cut(l, 1, inW)
			}
			rowY[len(lines)] = ri
			lines = append(lines, l+" "+scrollbar(len(m.data.branches), bw, m.bOff, k))
		}
	}
	return lines, rowY
}

// 把整个画面涂成深色背景（不依赖终端自己的主题）。设置 GV_BG=none 可关闭。
var cBase = lipgloss.Color("#1e1f29") // rgb(30, 31, 41)

func (m model) View() string {
	out := m.view()
	if m.w == 0 || os.Getenv("GV_BG") == "none" {
		return out
	}
	// 用一个占位字符取得「深色背景 + 默认前景」的转义序列（会按终端色彩能力自动降级）
	probe := lipgloss.NewStyle().Background(cBase).Foreground(cText).Render("X")
	base := probe[:strings.Index(probe, "X")]
	if lipgloss.ColorProfile() == termenv.TrueColor {
		base = "\x1b[48;2;30;31;41m\x1b[38;2;248;248;242m" // 精确的 rgb(30, 31, 41)，避免取整误差
	}
	lines := strings.Split(out, "\n")
	for len(lines) < m.h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		// 每次样式被重置后，立刻恢复深色背景
		l = strings.ReplaceAll(l, "\x1b[0m", "\x1b[0m"+base)
		l = strings.ReplaceAll(l, "\x1b[m", "\x1b[m"+base)
		if pad := m.w - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[i] = base + l + "\x1b[0m"
	}
	return strings.Join(lines[:m.h], "\n")
}

func keyHint(k, label string) string {
	return lipgloss.NewStyle().Background(cKeyBg).Foreground(cText).Bold(true).Render(" "+k+" ") + sGray.Render(" "+label)
}

func (m model) view() string {
	if m.w == 0 {
		return ""
	}
	w, h := m.w, m.h
	var lines []string

	if m.mode == modeDetail {
		lines = append(lines, sBold.Render(trunc(m.detailTtl, w)), sGray.Faint(true).Render(strings.Repeat("─", w)))
		end := min(len(m.detail), m.detailTop+m.detailBody())
		for _, l := range m.detail[m.detailTop:end] {
			lines = append(lines, sText.Render(trunc(l, w)))
		}
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, keyHint("j/k", "滚动")+"  "+keyHint("q", "返回"))
		return strings.Join(lines, "\n")
	}

	lines, _ = m.build()
	for len(lines) < h-2 {
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), h-2)]
	if m.mode == modeConfirm {
		lines = append(lines, sOrange.Render(trunc("⚠ "+m.confirm+" (y/n)", w)))
		lines = append(lines, keyHint("y", "确认")+"  "+keyHint("n", "取消"))
	} else {
		lines = append(lines, trunc(m.msg, w))
		lines = append(lines, trunc(strings.Join([]string{
			keyHint("j/k", "移动"), keyHint("Tab", "换栏"), keyHint("⏎", "打开"), keyHint("d", "删分支"), keyHint("p", "发布"), keyHint("q", "退出"),
		}, "  "), w))
	}
	return strings.Join(lines, "\n")
}

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	repo = abs
	if _, err := git("rev-parse", "--git-dir"); err != nil {
		fmt.Fprintf(os.Stderr, "不是 Git 仓库: %s\n", repo)
		os.Exit(1)
	}
	setupColor()
	m := model{data: loadData(), folded: map[string]bool{}}

	// 非交互（管道）：输出一次纯文本
	if fi, _ := os.Stdout.Stat(); fi.Mode()&os.ModeCharDevice == 0 {
		up := m.data.upstream
		if up == "" {
			up = "(未发布)"
		}
		fmt.Printf("%s  %s  更改:%d\n", m.data.head, up, m.data.changes)
		for _, c := range m.data.commits[:min(visibleCommits, len(m.data.commits))] {
			fmt.Printf("%s %s  %s\n", c.short, c.subject, ago(c.ts))
		}
		return
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// 很多终端（包括 T3 内置终端）支持真彩色，但没有设置 COLORTERM，
// 程序会误判成 256 色，把配色压成接近的灰色。这里默认按真彩色输出。
// GV_COLOR=256 强制 256 色；GV_COLOR=truecolor 强制真彩色。
// 只有 macOS 自带的 Terminal.app 不支持真彩色，会自动排除。
func setupColor() {
	switch os.Getenv("GV_COLOR") {
	case "256":
		lipgloss.SetColorProfile(termenv.ANSI256)
	case "truecolor":
		lipgloss.SetColorProfile(termenv.TrueColor)
	default:
		if os.Getenv("COLORTERM") == "" && os.Getenv("TERM_PROGRAM") != "Apple_Terminal" &&
			os.Getenv("NO_COLOR") == "" && strings.Contains(os.Getenv("TERM"), "256color") {
			lipgloss.SetColorProfile(termenv.TrueColor)
		}
	}
}
