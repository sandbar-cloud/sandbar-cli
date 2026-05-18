package output

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Styles are exported lipgloss styles for use by commands.
var (
	Green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	Red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	Bold   = lipgloss.NewStyle().Bold(true)
	Dim    = lipgloss.NewStyle().Faint(true)
)

// --- Spinner ---

type doneMsg struct {
	result string
	failed bool
}

type updateMsgMsg struct {
	msg string
}

type spinnerModel struct {
	spinner spinner.Model
	msg     string
	done    bool
	result  string
	failed  bool
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.done = true
			m.result = "Cancelled"
			m.failed = true
			return m, tea.Quit
		}
	case doneMsg:
		m.done = true
		m.result = msg.result
		m.failed = msg.failed
		return m, tea.Quit
	case updateMsgMsg:
		m.msg = msg.msg
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.done {
		if m.failed {
			return Red.Render("✗") + " " + m.result + "\n"
		}
		return Green.Render("✓") + " " + m.result + "\n"
	}
	return m.spinner.View() + " " + m.msg
}

// Spinner wraps a bubbletea program to provide a simple start/stop/fail API.
// In non-TTY environments (CI, redirected stderr), it falls back to plain
// stderr prints so output isn't swallowed.
type Spinner struct {
	interactive bool

	// interactive=true path:
	program   *tea.Program
	wg        sync.WaitGroup
	cancelled bool

	// CancelledC is closed when the user hits Ctrl+C. Never fires in
	// non-interactive mode.
	CancelledC chan struct{}
}

// NewSpinner starts a spinner with the given message. It renders to stderr.
func NewSpinner(msg string) *Spinner {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		fmt.Fprintf(os.Stderr, "  %s\n", msg)
		return &Spinner{interactive: false, CancelledC: make(chan struct{})}
	}

	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))

	m := spinnerModel{
		spinner: s,
		msg:     msg,
	}

	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	sp := &Spinner{interactive: true, program: p, CancelledC: make(chan struct{})}

	sp.wg.Add(1)
	go func() {
		defer sp.wg.Done()
		finalModel, _ := p.Run()
		if m, ok := finalModel.(spinnerModel); ok && m.result == "Cancelled" {
			sp.cancelled = true
			close(sp.CancelledC)
		}
	}()

	return sp
}

// Cancelled returns true if the user pressed Ctrl+C while the spinner was active.
func (s *Spinner) Cancelled() bool {
	if !s.interactive {
		return false
	}
	s.wg.Wait()
	return s.cancelled
}

// Stop stops the spinner and displays a green checkmark with the result message.
func (s *Spinner) Stop(result string) {
	if !s.interactive {
		fmt.Fprintf(os.Stderr, "  %s %s\n", Green.Render("✓"), result)
		return
	}
	s.program.Send(doneMsg{result: result, failed: false})
	s.wg.Wait()
}

// UpdateMsg updates the spinner's in-progress message without stopping it.
func (s *Spinner) UpdateMsg(msg string) {
	if !s.interactive {
		fmt.Fprintf(os.Stderr, "  %s\n", msg)
		return
	}
	s.program.Send(updateMsgMsg{msg: msg})
}

// Fail stops the spinner and displays a red ✗ mark with the result message.
func (s *Spinner) Fail(result string) {
	if !s.interactive {
		fmt.Fprintf(os.Stderr, "  %s %s\n", Red.Render("✗"), result)
		return
	}
	s.program.Send(doneMsg{result: result, failed: true})
	s.wg.Wait()
}

// --- Table ---

// Table prints a formatted table to stdout.
func Table(headers []string, rows [][]string) {
	// Calculate column widths from content.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Build columns.
	cols := make([]table.Column, len(headers))
	for i, h := range headers {
		cols[i] = table.Column{Title: h, Width: widths[i]}
	}

	// Build rows.
	tableRows := make([]table.Row, len(rows))
	for i, r := range rows {
		tableRows[i] = table.Row(r)
	}

	// Build styles: bold header, bottom border, no selection highlight.
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).BorderBottom(true)
	s.Selected = lipgloss.NewStyle()

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(tableRows),
		table.WithHeight(len(rows)),
		table.WithStyles(s),
	)

	fmt.Println(t.View())
}

// --- Utilities ---

// FormatBytes formats a byte count as a human-readable string.
func FormatBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatTimeAgo formats a time as a human-readable relative duration.
func FormatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}
