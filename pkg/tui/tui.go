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
	// Indentation of the detail view body.
	infoIndent = "  "
	// Width of the label column in the detail view.
	infoLabelWidth = 14
	// Fallback body width when the terminal size is unknown.
	infoFallbackWidth = 100
	// Width of the timestamp column in the event list ("15:04:05" plus a gap).
	infoTimeWidth = 10
	// Number of events shown in the detail view.
	infoMaxEvents = 10
	// Shown instead of a timestamp when something never happened.
	infoNever = "never"
	// Shown for an age below one second.
	infoJustNow = "just now"
)

var (
	// Column widths
	colWidths = []int{20, 35, 12, 6, 7, 25}

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

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9a9a9a")).
			Width(infoLabelWidth)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f0f0f0"))

	timeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))
)

// tickMsg is sent periodically to update the status
type tickMsg time.Time

// Model represents the TUI state.
type Model struct {
	proxies  []*proxy.Proxy
	rows     [][]string
	pacURL   string
	quitting bool
	showInfo bool
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
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			// Close the detail view first, quit only from the list.
			if m.showInfo {
				m.showInfo = false
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "enter", "i":
			if len(m.proxies) > 0 {
				m.showInfo = !m.showInfo
			}
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

	content := m.listView()
	if m.showInfo && m.cursor < len(m.proxies) {
		content = renderInfo(m.proxies[m.cursor].Status(), m.width, m.height, time.Now())
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// listView renders the proxy table with the PAC URL and status summary.
func (m Model) listView() string {
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
	fmt.Fprintf(&b, "  PAC URL: %s", pacURLStyle.Render(m.pacURL))
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
	b.WriteString(helpStyle.Render("  ↑/↓: Navigate • Enter: Info • q/Esc: Quit"))

	return b.String()
}

// renderInfo renders the detail page for a single proxy. The event list is
// trimmed to whatever vertical space is left, because bubbletea clips the frame
// to the terminal in alt-screen mode and would silently cut off the footer.
func renderInfo(status proxy.ProxyStatus, width, height int, now time.Time) string {
	if width <= 0 {
		width = infoFallbackWidth
	}
	valueStyle := lipgloss.NewStyle().Width(maxInt(20, width-len(infoIndent)-infoLabelWidth-1))

	var b strings.Builder

	field := func(label, value string) {
		row := lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render(label), valueStyle.Render(value))
		b.WriteString(infoIndent)
		b.WriteString(strings.ReplaceAll(row, "\n", "\n"+infoIndent))
		b.WriteString("\n")
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("🔗 %s — details", status.Name)))
	b.WriteString("\n\n")

	field("Domain", status.Domain)
	field("Status", formatStatus(status))
	field("Proxy port", fmt.Sprintf("%d", status.Port))
	field("Check URL", status.CheckEndpoint)
	field("Active node", orDash(status.ActiveNode))
	field("Nodes", formatNodes(status))
	if status.NodesError != "" {
		field("Node lookup", status.NodesError)
	}
	if status.PID > 0 {
		field("Tunnel PID", fmt.Sprintf("%d", status.PID))
	}
	if status.Restarts > 0 {
		field("Restarts", fmt.Sprintf("%d", status.Restarts))
	}

	b.WriteString("\n")
	field("Last check", formatLastCheck(status, now))
	if status.LastError != "" {
		field("Error", status.LastError)
	}
	if status.LastStartError != "" {
		field("Start error", status.LastStartError)
	}

	if len(status.Events) > 0 {
		eventStyle := lipgloss.NewStyle().Width(maxInt(20, width-len(infoIndent)-infoTimeWidth))

		// Render newest first, so the oldest events are the ones dropped.
		rows := make([]string, 0, infoMaxEvents)
		for i := len(status.Events) - 1; i >= 0 && len(rows) < infoMaxEvents; i-- {
			event := status.Events[i]
			stamp := timeStyle.Render(event.Time.Format("15:04:05")) + "  "
			row := lipgloss.JoinHorizontal(lipgloss.Top, stamp, eventStyle.Render(event.Message))
			rows = append(rows, infoIndent+strings.ReplaceAll(row, "\n", "\n"+infoIndent))
		}

		// Lines still free after the blank line, the section header and the
		// two lines the footer occupies.
		free := len(rows)
		if height > 0 {
			free = height - lineCount(b.String()) - 4
		}

		shown, used := 0, 0
		for _, row := range rows {
			rowLines := lineCount(row)
			if used+rowLines > free {
				break
			}
			used += rowLines
			shown++
		}

		// Make room for the note about what was left out.
		omitted := len(status.Events) - shown
		for omitted > 0 && shown > 0 && used+1 > free {
			shown--
			used -= lineCount(rows[shown])
			omitted++
		}

		if shown > 0 {
			b.WriteString("\n")
			b.WriteString(infoIndent)
			b.WriteString(sectionStyle.Render("Recent events"))
			b.WriteString("\n")

			for _, row := range rows[:shown] {
				b.WriteString(row)
				b.WriteString("\n")
			}

			if omitted > 0 {
				b.WriteString(infoIndent)
				b.WriteString(timeStyle.Render(fmt.Sprintf("… %d older event(s) not shown", omitted)))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString(helpStyle.Render(infoIndent + "↑/↓: Prev/Next • Enter/Esc: Back • q: Quit"))

	return b.String()
}

// lineCount returns how many terminal lines a rendered block occupies.
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// formatNodes describes the known Teleport nodes of a proxy.
func formatNodes(status proxy.ProxyStatus) string {
	if len(status.Nodes) == 0 {
		return "none"
	}
	return fmt.Sprintf("%d known: %s", len(status.Nodes), strings.Join(status.Nodes, ", "))
}

// formatLastCheck summarizes the most recent health check in one line.
func formatLastCheck(status proxy.ProxyStatus, now time.Time) string {
	if status.LastCheck.IsZero() {
		if status.NodeCount == 0 {
			return infoNever + " - no nodes available"
		}
		return infoNever
	}

	parts := []string{formatAge(status.LastCheck, now)}
	if status.LastDuration > 0 {
		parts = append(parts, "took "+status.LastDuration.Round(time.Millisecond).String())
	}
	parts = append(parts, "HTTP "+formatStatusCode(status.LastStatusCode))

	return strings.Join(parts, " · ")
}

// formatAge renders how long ago something happened, relative to now.
func formatAge(t, now time.Time) string {
	if t.IsZero() {
		return infoNever
	}
	age := now.Sub(t)
	if age < time.Second {
		return infoJustNow
	}
	return age.Round(time.Second).String() + " ago"
}

// formatStatusCode renders an HTTP status code, or a dash if there was none.
func formatStatusCode(code int) string {
	if code == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", code)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
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
