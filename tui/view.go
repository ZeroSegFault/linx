package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED") // purple
	secondaryColor = lipgloss.Color("#06B6D4") // cyan
	mutedColor     = lipgloss.Color("#6B7280") // gray
	successColor   = lipgloss.Color("#10B981") // green
	errorColor     = lipgloss.Color("#EF4444") // red
	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(primaryColor).
			Padding(0, 1)

	statusTextStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	toolCallStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	toolDoneStyle = lipgloss.NewStyle().
			Foreground(successColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	confirmStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true)

)

func (m *model) View() string {
	if !m.ready {
		return "\n  Initializing Linx..."
	}

	var b strings.Builder

	// Title bar — show active model
	modelInfo := m.cfg.Provider.Model
	if modelInfo == "" {
		modelInfo = "no model"
	}
	title := titleStyle.Render(fmt.Sprintf("🐧 Linx — %s", modelInfo))
	b.WriteString(title)
	b.WriteString("\n")

	// Output viewport
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Status bar
	var statusContent string
	switch {
	case m.recovering:
		statusContent = confirmStyle.Render("🔄 Restore previous session? [r]estore / [n]ew")
	case m.confirmPending:
		statusContent = confirmStyle.Render("⚠ Confirm action? [y/n]")
	case m.thinking:
		statusContent = fmt.Sprintf("%s %s", m.spinner.View(), statusTextStyle.Render(m.statusMsg))
	default:
		if m.agent != nil {
			pct := m.agent.ContextUsagePercent()
			contextStr := fmt.Sprintf("[%d%%]", pct)
			if pct >= 90 {
				contextStr = lipgloss.NewStyle().Foreground(errorColor).Render(contextStr)
			} else if pct >= 75 {
				contextStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render(contextStr)
			} else {
				contextStr = lipgloss.NewStyle().Foreground(successColor).Render(contextStr)
			}
			statusContent = statusBarStyle.Render("Ready") + " " + contextStr
		} else {
			statusContent = statusBarStyle.Render("Ready")
		}
	}
	b.WriteString(statusContent)
	b.WriteString("\n")

	// Input area
	if m.confirmPending {
		b.WriteString(confirmStyle.Render(m.confirmDesc))
		b.WriteString("\n")
		b.WriteString(confirmStyle.Render("Press y to confirm, n to deny: "))
	} else {
		b.WriteString(inputBorderStyle.Render(m.textarea.View()))
	}
	b.WriteString("\n")

	// Help line
	b.WriteString(helpStyle.Render("enter: submit • ctrl+l: clear • ctrl+c: quit"))

	return b.String()
}
