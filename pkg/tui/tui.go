// Package tui provides a terminal user interface for linkmeup.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

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
	// Column widths
	colWidths = []int{20, 35, 12, 6, 7, 25}

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
	rows     [][]string
	pacURL   string
	quitting bool
	width    int
	height   int
	lastTick time.Time
	cursor   int
}

// New creates a new TUI model.
func New(proxies []*proxy.Proxy, pacPort int) Model {
	return Model{
		proxies:  proxies,
		rows:     buildRows(proxies),
		pacURL:   fmt.Sprintf("http://localhost:%d/proxy.pac", pacPort),
		lastTick: time.Now(),
	}
}

func buildRows(proxies []*proxy.Proxy) [][]string {
	rows := make([][]string, 0, len(proxies))
	for _, p := range proxies {
		status := p.Status()
		nodeStr := status.ActiveNode
		if nodeStr == "" {
			nodeStr = "-"
		}
		statusStr := formatStatus(status)
		rows = append(rows, []string{
			status.Name,
			status.Domain,
			statusStr,
			fmt.Sprintf("%d", status.Port),
			fmt.Sprintf("%d", status.NodeCount),
			nodeStr,
		})
	}
	return rows
}

func formatStatus(status proxy.ProxyStatus) string {
	switch {
	case status.NodeCount == 0:
		return pendingStyle.Render("- No Nodes")
	case status.Healthy:
		return healthyStyle.Render("✓ Healthy")
	default:
		return unhealthyStyle.Render("✗ Unhealthy")
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.proxies)-1 {
				m.cursor++
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		m.rows = buildRows(m.proxies)
		m.lastTick = time.Time(msg)
		return m, tickCmd()
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("🔗 linkmeup - Installation Proxies"))
	b.WriteString("\n\n")

	// Build table using lipgloss/v2/table
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#5a4fcf"))).
		Headers("Name", "Domain", "Status", "Port", "Nodes", "Active Node").
		Width(sum(colWidths) + 7). // account for border characters
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if row == m.cursor {
				return selectedStyle
			}
			return lipgloss.NewStyle()
		})

	for _, row := range m.rows {
		t.Row(row...)
	}

	b.WriteString(t.Render())
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

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func sum(a []int) int {
	s := 0
	for _, v := range a {
		s += v
	}
	return s
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
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
