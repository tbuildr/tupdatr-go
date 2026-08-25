package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Palette
// ---------------------------------------------------------------------------

const (
	colFrom   = "#8B5CF6" // violet – gradient start / accent
	colTo     = "#22D3EE" // cyan   – gradient end / accent
	colOK     = "#4ADE80" // green  – success
	colBad    = "#F87171" // red    – failure
	colDim    = "#6B7280" // grey   – secondary text
	colCursor = "#FDE047" // yellow – cursor marker
)

// Sentinel for "not yet run"; real exit codes are >= -1 (-1 = killed by a
// signal, mirroring waitpid semantics).
const statusNotRun = math.MinInt32

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type lineMsg string

type stepDoneMsg struct {
	idx    int
	status int
}

type pulseTickMsg struct{}

const (
	pulseFrameInterval = 33 * time.Millisecond // ~30 fps
	maxKeptLines       = 5000
)

func pulseTick() tea.Cmd {
	return tea.Tick(pulseFrameInterval, func(time.Time) tea.Msg { return pulseTickMsg{} })
}

// spring drives the "glow" of the running panel's border: a harmonica
// spring easing pulse toward target whenever the state changes (start
// running -> brighten, finish -> settle back).
var spring = harmonica.NewSpring(harmonica.FPS(60), 6.0, 0.55)

// programRef lets background goroutines stream output into the event loop.
var programRef *tea.Program

// ---------------------------------------------------------------------------
// Help bindings (rendered through the bubbles/help component)
// ---------------------------------------------------------------------------

var (
	kbMove   = key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("↑/↓", "move"))
	kbToggle = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle"))
	kbAll    = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all"))
	kbRun    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run selected"))
	kbQuit   = key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit"))
	kbAbort  = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "abort"))
	kbBack   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back to menu"))
	kbExit   = key.NewBinding(key.WithKeys("q", "enter", "ctrl+c"), key.WithHelp("q/enter", "exit"))

	menuShortHelp = []key.Binding{kbMove, kbToggle, kbAll, kbRun, kbQuit}
	runShortHelp  = []key.Binding{kbAbort}
	doneShortHelp = []key.Binding{kbBack, kbExit}
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type screen int

const (
	screenMenu screen = iota
	screenRunning
	screenDone
)

type tui struct {
	dryRun bool

	screen  screen
	cursor  int
	checked []bool

	// Run state
	order    []int
	pos      int      // index into order of the last completed step
	status   []int    // per-action exit status; statusNotRun until run
	current  int      // action index in flight, -1 when idle
	lines    []string // captured output of the current run
	ctx      context.Context
	cancel   context.CancelFunc
	exitCode int // decided at quit time

	vp   viewport.Model
	prog progress.Model
	spin spinner.Model
	help help.Model

	pulse    float64
	pulseVel float64
	target   float64

	width  int
	height int
}

func newTUI(dryRun bool) tui {
	prog := progress.New(
		progress.WithGradient(colFrom, colTo),
		progress.WithSpringOptions(6.0, 0.55), // tuned harmonica feel
	)
	prog.SetPercent(0)
	m := tui{
		dryRun:  dryRun,
		cursor:  0,
		checked: make([]bool, len(actions)),
		status:  make([]int, len(actions)),
		current: -1,
		vp:      viewport.New(60, 10),
		prog:    prog,
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:    help.New(),
	}
	for i := range m.status {
		m.status[i] = statusNotRun
	}
	return m
}

func (m tui) Init() tea.Cmd { return nil }

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case spinner.TickMsg:
		if m.screen != screenRunning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, tea.Batch(cmd, m.spin.Tick)

	case pulseTickMsg:
		m.pulse, m.pulseVel = spring.Update(m.pulse, m.pulseVel, m.target)
		if math.Abs(m.pulse-m.target) > 0.001 || math.Abs(m.pulseVel) > 0.001 {
			return m, pulseTick()
		}
		m.pulse, m.pulseVel = m.target, 0
		return m, nil

	case lineMsg:
		m.appendLine(string(msg))
		return m, nil

	case stepDoneMsg:
		return m.updateStepDone(msg)

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m tui) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {

	case screenMenu:
		switch msg.String() {
		case "ctrl+c", "q":
			m.exitCode = 0
			return m, tea.Quit
		case "up", "k":
			m.cursor = (m.cursor - 1 + len(actions)) % len(actions)
		case "down", "j":
			m.cursor = (m.cursor + 1) % len(actions)
		case " ":
			m.checked[m.cursor] = !m.checked[m.cursor]
		case "a":
			for i := range m.checked {
				m.checked[i] = true
			}
		case "enter":
			return m.beginRun()
		}

	case screenRunning:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			m.exitCode = 130 // conventional SIGINT status
			return m, tea.Quit
		}

	case screenDone:
		switch msg.String() {
		case "esc":
			m.screen = screenMenu
			m.current = -1
			m.lines = nil
			m.vp.SetContent("")
		case "q", "enter", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// beginRun mirrors the C menu: run every checked action in table order, or
// just the highlighted one if nothing is checked.
func (m tui) beginRun() (tea.Model, tea.Cmd) {
	m.order = nil
	for i := range actions {
		if m.checked[i] {
			m.order = append(m.order, i)
		}
	}
	if len(m.order) == 0 {
		m.order = []int{m.cursor}
	}
	for i := range m.status {
		m.status[i] = statusNotRun
	}
	m.pos = 0
	m.current = m.order[0]
	m.lines = nil
	m.vp.SetContent("")
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.screen = screenRunning
	first := m.order[0]
	return m, tea.Batch(
		pulseTick(),
		m.spin.Tick,
		runStepCmd(first, m.ctx, m.dryRun),
	)
}

func (m tui) updateStepDone(msg stepDoneMsg) (tea.Model, tea.Cmd) {
	m.status[msg.idx] = msg.status
	anim := m.prog.SetPercent(float64(m.pos+1) / float64(len(m.order))) // spring-animated

	// Like the C version: keep going through every selected action even if
	// one fails; the last nonzero status becomes the overall exit code.
	if m.pos+1 < len(m.order) {
		m.pos++
		next := m.order[m.pos]
		m.current = next
		m.appendLine("")
		return m, tea.Batch(anim, runStepCmd(next, m.ctx, m.dryRun))
	}

	overall := 0
	for _, idx := range m.order {
		if st := m.status[idx]; st != 0 {
			overall = st
		}
	}
	m.exitCode = overall
	m.screen = screenDone
	m.target = 0
	m.current = -1
	return m, anim
}

func runStepCmd(idx int, ctx context.Context, dryRun bool) tea.Cmd {
	return func() tea.Msg {
		st := RunAction(ctx, actions[idx], dryRun, func(line string) {
			if programRef != nil {
				programRef.Send(lineMsg(line))
			}
		})
		return stepDoneMsg{idx: idx, status: st}
	}
}

func (m *tui) appendLine(line string) {
	m.lines = append(m.lines, line)
	if len(m.lines) > maxKeptLines {
		m.lines = m.lines[len(m.lines)-maxKeptLines:]
	}
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	m.vp.GotoBottom()
}

func (m *tui) resize() {
	bodyW := m.width - 8 // outer margin + box padding
	if bodyW < 20 {
		bodyW = 20
	}
	m.prog.Width = bodyW - 6
	h := m.height - 16
	if h < 4 {
		h = 4
	}
	if h > 24 {
		h = 24
	}
	m.vp.Width = bodyW - 6
	m.vp.Height = h
	m.help.Width = bodyW
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m tui) View() string {
	if m.width == 0 {
		return "\n  initializing…"
	}
	var body string
	switch m.screen {
	case screenMenu:
		body = m.viewMenu()
	case screenRunning:
		body = m.viewSteps(false)
	default:
		body = m.viewSteps(true)
	}
	return "\n" + lipgloss.NewStyle().MarginLeft(2).Render(body)
}

func (m tui) titleBar(subtitle string) string {
	title := gradientText("⚡ tupdatr", colFrom, colTo)
	sub := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render(subtitle)
	gap := strings.Repeat(" ", maxInt(1, m.width-8-lipgloss.Width(title)-lipgloss.Width(sub)))
	return title + gap + sub + "\n\n"
}

func (m tui) viewMenu() string {
	var b strings.Builder
	b.WriteString(m.titleBar(menuSubtitle(m.dryRun)))

	boxOn := lipgloss.NewStyle().Foreground(lipgloss.Color(colOK)).Bold(true)
	boxOff := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	lblStyle := lipgloss.NewStyle().Bold(true)
	lblCur := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colCursor))
	curSty := lipgloss.NewStyle().Foreground(lipgloss.Color(colCursor))
	descSty := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))

	labelCol := minLabelCol()
	for i, a := range actions {
		cur := "  "
		if i == m.cursor {
			cur = curSty.Render("❯ ")
		}
		check := boxOff.Render("[ ]")
		if m.checked[i] {
			check = boxOn.Render("[✓]")
		}
		label := lblStyle.Render(a.Label)
		if i == m.cursor {
			label = lblCur.Render(a.Label)
		}
		pad := strings.Repeat(" ", labelCol-lipgloss.Width(a.Label))
		b.WriteString(cur + check + " " + label + pad + "  " + descSty.Render(a.Description) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpLine(m.help, menuShortHelp))
	return boxed(b.String(), mixColor(colFrom, colTo, 0.35+m.pulse*0.65))
}

func (m tui) viewSteps(done bool) string {
	var b strings.Builder
	subtitle := fmt.Sprintf("%d/%d", minInt(len(m.order), m.pos+1), len(m.order))
	if done {
		subtitle = "complete"
	}
	b.WriteString(m.titleBar(runSubtitle(m.dryRun, subtitle)))

	pending := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	okSty := lipgloss.NewStyle().Foreground(lipgloss.Color(colOK)).Bold(true)
	badSty := lipgloss.NewStyle().Foreground(lipgloss.Color(colBad)).Bold(true)
	runSty := lipgloss.NewStyle().Foreground(lipgloss.Color(colTo)).Bold(true)

	for _, idx := range m.order {
		st := m.status[idx]
		line := ""
		switch {
		case st == statusNotRun && idx != m.current:
			line = "  " + pending.Render("○ "+actions[idx].Label)
		case st == 0:
			line = "  " + okSty.Render("✓ "+actions[idx].Label)
		case st < 0:
			line = "  " + badSty.Render(fmt.Sprintf("✗ %s (killed by signal %d)", actions[idx].Label, -st))
		default:
			line = "  " + badSty.Render(fmt.Sprintf("✗ %s (exit %d)", actions[idx].Label, st))
		}
		if !done && idx == m.current {
			line = "  " + m.spin.View() + " " + runSty.Render(actions[idx].Label+" …")
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	panelBorder := mixColor(colFrom, colTo, 0.25+m.pulse*0.75)
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorder)).
		Padding(0, 1).
		Render(m.vp.View())
	b.WriteString(panel + "\n\n")

	if done {
		failed := 0
		for _, idx := range m.order {
			if m.status[idx] != 0 {
				failed++
			}
		}
		summary := lipgloss.NewStyle().
			Foreground(lipgloss.Color(colOK)).
			Bold(true).
			Render("✨ all updates finished")
		if failed > 0 {
			summary = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colBad)).
				Bold(true).
				Render(fmt.Sprintf("finished — %d of %d failed", failed, len(m.order)))
		}
		b.WriteString(summary + "\n\n")
		b.WriteString(helpLine(m.help, doneShortHelp))
	} else {
		b.WriteString(m.prog.View()) // harmonica-spring animated fill
		b.WriteString("\n\n")
		b.WriteString(helpLine(m.help, runShortHelp))
	}
	return boxed(b.String(), panelBorder)
}

func helpLine(h help.Model, bindings []key.Binding) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(colDim)).
		Render(h.ShortHelpView(bindings))
}

func boxed(content, border string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(border)).
		Padding(1, 2).
		Render(content)
}

func menuSubtitle(dryRun bool) string {
	if dryRun {
		return "dry-run mode"
	}
	return "update everything, gently"
}

func runSubtitle(dryRun bool, s string) string {
	if dryRun {
		return "dry-run · " + s
	}
	return s
}

func minLabelCol() int {
	w := 8
	for _, a := range actions {
		if lipgloss.Width(a.Label) > w {
			w = lipgloss.Width(a.Label)
		}
	}
	return w
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Color helpers
// ---------------------------------------------------------------------------

// gradientText renders each rune of s interpolated between two hex colors.
func gradientText(s, from, to string) string {
	runes := []rune(s)
	var b strings.Builder
	n := len(runes)
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		st := lipgloss.NewStyle().
			Foreground(lipgloss.Color(mixColor(from, to, t))).
			Bold(true)
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

func hexRGB(c string) (float64, float64, float64) {
	c = strings.TrimPrefix(c, "#")
	if len(c) != 6 {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(c, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return float64((v >> 16) & 0xFF), float64((v >> 8) & 0xFF), float64(v & 0xFF)
}

func mixColor(a, b string, t float64) string {
	r1, g1, b1 := hexRGB(a)
	r2, g2, b2 := hexRGB(b)
	r := uint8(math.Round(r1 + (r2-r1)*t))
	g := uint8(math.Round(g1 + (g2-g1)*t))
	bl := uint8(math.Round(b1 + (b2-b1)*t))
	return fmt.Sprintf("#%02x%02x%02x", r, g, bl)
}

// ---------------------------------------------------------------------------
// Entry point used by main.go
// ---------------------------------------------------------------------------

func runTUI(dryRun bool) (int, error) {
	p := tea.NewProgram(newTUI(dryRun), tea.WithAltScreen())
	programRef = p
	final, err := p.Run()
	if err != nil {
		return 1, err
	}
	if tm, ok := final.(tui); ok {
		return tm.exitCode, nil
	}
	return 0, nil
}
