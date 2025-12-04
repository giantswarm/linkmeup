// Package tui provides a terminal user interface for linkmeup.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	btable "github.com/evertras/bubble-table/table"
	"github.com/giantswarm/linkmeup/pkg/proxy"
)

const (
	columnKeyName       = "name"
	columnKeyDomain     = "domain"
	columnKeyStatus     = "status"
	columnKeyPort       = "port"
	columnKeyNodes      = "nodes"
	columnKeyActiveNode = "activeNode"
)

var (
	// Styles
	baseStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5a4fcf"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f0f0f0")).
			Background(lipgloss.Color("#5a4fcf")).
			Padding(0, 1).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginTop(1)

	healthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Bold(true)

	unhealthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87")).
			Bold(true)

	pendingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFAF00")).
			Bold(true)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f0f0f0")).
			Background(lipgloss.Color("#5a4fcf"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f0f0f0")).
			Background(lipgloss.Color("#7c3aed")).
			Bold(true)

	pacURLStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5a4fcf")).
			Bold(true).
			Underline(true)
)

// tickMsg is sent periodically to update the status
type tickMsg time.Time

// Model represents the TUI state.
type Model struct {
	proxies  []*proxy.Proxy
	table    btable.Model
	pacURL   string
	quitting bool
	width    int
	height   int
	lastTick time.Time
}

// New creates a new TUI model.
func New(proxies []*proxy.Proxy, pacPort int) Model {
	columns := []btable.Column{
		btable.NewColumn(columnKeyName, "Name", 20),
		btable.NewColumn(columnKeyDomain, "Domain", 35),
		btable.NewColumn(columnKeyStatus, "Status", 12),
		btable.NewColumn(columnKeyPort, "Port", 6),
		btable.NewColumn(columnKeyNodes, "Nodes", 7),
		btable.NewColumn(columnKeyActiveNode, "Active Node", 25),
	}

	rows := buildRows(proxies)

	// Use a hidden border style so that bubble-table doesn't render any
	// internal borders; we only want the outer box from baseStyle.
	borderlessStyle := lipgloss.NewStyle().BorderStyle(lipgloss.HiddenBorder())

	t := btable.New(columns).
		WithRows(rows).
		WithBaseStyle(borderlessStyle).
		HighlightStyle(selectedStyle).
		WithHeaderVisibility(false)

	return Model{
		proxies:  proxies,
		table:    t,
		pacURL:   fmt.Sprintf("http://localhost:%d/proxy.pac", pacPort),
		lastTick: time.Now(),
	}
}

func buildRows(proxies []*proxy.Proxy) []btable.Row {
	rows := make([]btable.Row, 0, len(proxies))
	for _, p := range proxies {
		status := p.Status()
		nodeStr := status.ActiveNode
		if nodeStr == "" {
			nodeStr = "-"
		}
		nodeCountStr := fmt.Sprintf("%d", status.NodeCount)
		rows = append(rows, btable.NewRow(btable.RowData{
			columnKeyName:       status.Name,
			columnKeyDomain:     status.Domain,
			columnKeyStatus:     newStatusCell(status),
			columnKeyPort:       fmt.Sprintf("%d", status.Port),
			columnKeyNodes:      nodeCountStr,
			columnKeyActiveNode: nodeStr,
		}))
	}
	return rows
}

func newStatusCell(status proxy.ProxyStatus) any {
	switch {
	case status.NodeCount == 0:
		return btable.NewStyledCell("- No Nodes", pendingStyle)
	case status.Healthy:
		return btable.NewStyledCell("✓ Healthy", healthyStyle)
	default:
		return btable.NewStyledCell("✗ Unhealthy", unhealthyStyle)
	}
}

func renderHeaderRow() string {
	// Keep widths in sync with the bubble-table column definitions in New().
	// Use left alignment for all headers to match the data columns.
	// bubble-table renders a leading space before the first column, so we add
	// one here as well to keep header and body perfectly aligned.
	return " " + fmt.Sprintf(
		"%-20s %-35s %-12s %-6s %-7s %-25s",
		"Name",
		"Domain",
		"Status",
		"Port",
		"Nodes",
		"Active Node",
	)
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), tea.EnterAltScreen)
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		// Update the table rows with fresh status
		m.table = m.table.WithRows(buildRows(m.proxies))
		m.lastTick = time.Time(msg)
		return m, tickCmd()
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("🔗 linkmeup - Installation Proxies"))
	b.WriteString("\n\n")

	// Table (custom header row + bubble-table body, all inside outer border)
	header := headerStyle.Render(renderHeaderRow())
	body := m.table.View()

	// bubble-table still renders a top border line; trim that so we don't get
	// a mismatched horizontal rule between our custom header and the rows.
	if body != "" {
		lines := strings.Split(body, "\n")
		if len(lines) > 1 {
			body = strings.Join(lines[1:], "\n")
		}
	}

	b.WriteString(baseStyle.Render(header + "\n" + body))
	b.WriteString("\n")

	// PAC URL info
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  PAC URL: %s", pacURLStyle.Render(m.pacURL)))
	b.WriteString("\n")

	// Status counts - use same symbols as table
	healthy, unhealthy, noNodes := countStatus(m.proxies)
	statusLine := fmt.Sprintf("  %s %d healthy  %s %d unhealthy",
		healthyStyle.Render("✓"), healthy,
		unhealthyStyle.Render("✗"), unhealthy)
	if noNodes > 0 {
		statusLine += fmt.Sprintf("  %s %d no nodes", pendingStyle.Render("-"), noNodes)
	}
	b.WriteString(statusLine)
	b.WriteString("\n")

	// Help
	b.WriteString(helpStyle.Render("  ↑/↓: Navigate • q/Esc: Quit"))

	return b.String()
}

func countStatus(proxies []*proxy.Proxy) (healthy, unhealthy, noNodes int) {
	for _, p := range proxies {
		status := p.Status()
		if status.NodeCount == 0 {
			noNodes++
		} else if status.Healthy {
			healthy++
		} else {
			unhealthy++
		}
	}
	return
}

// Run starts the TUI.
func Run(proxies []*proxy.Proxy, pacPort int) error {
	m := New(proxies, pacPort)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
