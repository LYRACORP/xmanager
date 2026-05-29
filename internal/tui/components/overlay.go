package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type HelpOverlay struct {
	Title    string
	Bindings []KeyBinding
	Width    int
	Height   int
	Visible  bool
}

func NewHelpOverlay(title string, bindings []KeyBinding) HelpOverlay {
	return HelpOverlay{Title: title, Bindings: bindings}
}

func (h HelpOverlay) View() string {
	if !h.Visible {
		return ""
	}

	var lines []string
	lines = append(lines, theme.TitleStyle().Render(h.Title))
	lines = append(lines, "")
	for _, b := range h.Bindings {
		key := theme.KeyStyle().Width(12).Render(b.Key)
		desc := theme.DescStyle().Render(b.Desc)
		lines = append(lines, "  "+key+"  "+desc)
	}
	lines = append(lines, "")
	lines = append(lines, theme.MutedText().Render("  Press ? or esc to close"))

	content := strings.Join(lines, "\n")
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Current.Border).
		Background(theme.Current.Surface).
		Padding(theme.PadMD).
		Width(h.Width - 4).
		Render(content)

	return lipgloss.Place(h.Width, h.Height, lipgloss.Center, lipgloss.Center, panel)
}
