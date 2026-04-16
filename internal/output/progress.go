package output

import (
	"fmt"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles for progress bars.
var (
	barFilled = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	barEmpty  = lipgloss.NewStyle().Faint(true)
)

// --- Bubbletea messages ---

type blobStartedMsg struct {
	id       int
	filePath string
	size     int64
}

type blobUploadedMsg struct {
	id       int
	uploaded int64
}

type blobDoneMsg struct {
	id int
}

type blobFinishMsg struct {
	message string
	failed  bool
}

// --- Per-blob state ---

type blobState struct {
	filePath string
	size     int64
	uploaded int64
}

// --- Bubbletea model ---

type blobProgressModel struct {
	blobs       map[int]*blobState
	activeOrder []int
	completed   int
	total       int
	totalBytes  int64
	pushedBytes int64
	finished    bool
	finalMsg    string
	failed      bool
}

func (m blobProgressModel) Init() tea.Cmd { return nil }

func (m blobProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.finished = true
			m.finalMsg = "Cancelled"
			m.failed = true
			return m, tea.Quit
		}
	case blobStartedMsg:
		m.blobs[msg.id] = &blobState{
			filePath: msg.filePath,
			size:     msg.size,
		}
		m.activeOrder = append(m.activeOrder, msg.id)
	case blobUploadedMsg:
		if b, ok := m.blobs[msg.id]; ok {
			b.uploaded = msg.uploaded
		}
	case blobDoneMsg:
		if b, ok := m.blobs[msg.id]; ok {
			m.completed++
			m.pushedBytes += b.size
		}
		for i, id := range m.activeOrder {
			if id == msg.id {
				m.activeOrder = append(m.activeOrder[:i], m.activeOrder[i+1:]...)
				break
			}
		}
		delete(m.blobs, msg.id)
	case blobFinishMsg:
		m.finished = true
		m.finalMsg = msg.message
		m.failed = msg.failed
		return m, tea.Quit
	}
	return m, nil
}

func (m blobProgressModel) View() string {
	if m.finished {
		if m.failed {
			return Red.Render("✗") + " " + m.finalMsg + "\n"
		}
		return Green.Render("✓") + " " + m.finalMsg + "\n"
	}

	var b strings.Builder

	for _, id := range m.activeOrder {
		blob := m.blobs[id]
		name := padRight(truncatePath(blob.filePath, 30), 30)
		bar := renderBar(blob.uploaded, blob.size, 25)
		pct := 0
		if blob.size > 0 {
			pct = int(blob.uploaded * 100 / blob.size)
		}
		fmt.Fprintf(&b, "  %s %s %3d%%  %s / %s\n",
			name, bar, pct,
			padLeft(FormatBytes(blob.uploaded), 8),
			FormatBytes(blob.size))
	}

	if m.total > 0 {
		fmt.Fprintf(&b, "\n  %d/%d pushed (%s / %s)",
			m.completed, m.total,
			FormatBytes(m.pushedBytes),
			FormatBytes(m.totalBytes))
	}

	return b.String()
}

// --- Helpers ---

func renderBar(current, total int64, width int) string {
	if total <= 0 {
		return barFilled.Render(strings.Repeat("█", width))
	}
	filled := int(current * int64(width) / total)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return barFilled.Render(strings.Repeat("█", filled)) +
		barEmpty.Render(strings.Repeat("░", width-filled))
}

func truncatePath(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return "…" + s[len(s)-maxLen+1:]
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// --- BlobProgress Public API ---

// BlobProgress manages a multi-line docker-push style progress display.
type BlobProgress struct {
	program *tea.Program
	wg      sync.WaitGroup
}

// NewBlobProgress creates and starts a blob progress display.
func NewBlobProgress(total int, totalBytes int64) *BlobProgress {
	m := blobProgressModel{
		blobs:      make(map[int]*blobState),
		total:      total,
		totalBytes: totalBytes,
	}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	bp := &BlobProgress{program: p}
	bp.wg.Add(1)
	go func() {
		defer bp.wg.Done()
		p.Run()
	}()

	return bp
}

// BlobStarted reports that a blob has started uploading.
func (bp *BlobProgress) BlobStarted(id int, filePath string, size int64) {
	bp.program.Send(blobStartedMsg{id: id, filePath: filePath, size: size})
}

// BlobUploaded reports byte-level upload progress for a blob.
func (bp *BlobProgress) BlobUploaded(id int, uploaded int64) {
	bp.program.Send(blobUploadedMsg{id: id, uploaded: uploaded})
}

// BlobDone reports that a blob has finished uploading.
func (bp *BlobProgress) BlobDone(id int) {
	bp.program.Send(blobDoneMsg{id: id})
}

// Stop finishes the progress display with a success message.
func (bp *BlobProgress) Stop(message string) {
	bp.program.Send(blobFinishMsg{message: message})
	bp.wg.Wait()
}

// Fail finishes the progress display with an error message.
func (bp *BlobProgress) Fail(message string) {
	bp.program.Send(blobFinishMsg{message: message, failed: true})
	bp.wg.Wait()
}
