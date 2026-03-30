package theme

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Name       string
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Accent     lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	Critical   lipgloss.Color
	Muted      lipgloss.Color
	Text       lipgloss.Color
	TextDim    lipgloss.Color
	Background lipgloss.Color
	Surface    lipgloss.Color
	Border     lipgloss.Color
}

var Dark = Theme{
	Name:       "dark",
	Primary:    lipgloss.Color("#7C3AED"),
	Secondary:  lipgloss.Color("#06B6D4"),
	Accent:     lipgloss.Color("#F59E0B"),
	Success:    lipgloss.Color("#10B981"),
	Warning:    lipgloss.Color("#F59E0B"),
	Error:      lipgloss.Color("#EF4444"),
	Critical:   lipgloss.Color("#DC2626"),
	Muted:      lipgloss.Color("#6B7280"),
	Text:       lipgloss.Color("#F9FAFB"),
	TextDim:    lipgloss.Color("#9CA3AF"),
	Background: lipgloss.Color("#111827"),
	Surface:    lipgloss.Color("#1F2937"),
	Border:     lipgloss.Color("#374151"),
}

var Light = Theme{
	Name:       "light",
	Primary:    lipgloss.Color("#7C3AED"),
	Secondary:  lipgloss.Color("#0891B2"),
	Accent:     lipgloss.Color("#D97706"),
	Success:    lipgloss.Color("#059669"),
	Warning:    lipgloss.Color("#D97706"),
	Error:      lipgloss.Color("#DC2626"),
	Critical:   lipgloss.Color("#B91C1C"),
	Muted:      lipgloss.Color("#9CA3AF"),
	Text:       lipgloss.Color("#111827"),
	TextDim:    lipgloss.Color("#6B7280"),
	Background: lipgloss.Color("#FFFFFF"),
	Surface:    lipgloss.Color("#F3F4F6"),
	Border:     lipgloss.Color("#D1D5DB"),
}

var Current = Dark

func SetTheme(name string) {
	switch name {
	case "light":
		Current = Light
	default:
		Current = Dark
	}
}

// --- Reusable Style Functions ---

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(Current.Primary).
		PaddingLeft(1)
}

func TitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(Current.Text)
}

func SubtitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.TextDim)
}

func PanelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Current.Border).
		Padding(0, 1)
}

func ActivePanelStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(Current.Primary)
}

func BadgeStyle(color lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(color).
		Padding(0, 1).
		Bold(true)
}

func SuccessBadge() lipgloss.Style { return BadgeStyle(Current.Success) }
func WarningBadge() lipgloss.Style { return BadgeStyle(Current.Warning) }
func ErrorBadge() lipgloss.Style   { return BadgeStyle(Current.Error) }
func InfoBadge() lipgloss.Style    { return BadgeStyle(Current.Secondary) }

func StatusDot(running bool) string {
	if running {
		return lipgloss.NewStyle().Foreground(Current.Success).Render("●")
	}
	return lipgloss.NewStyle().Foreground(Current.Error).Render("●")
}

func MutedText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Muted)
}

func ErrorText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Error)
}

func SuccessText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Success)
}

func WarningText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Current.Warning)
}

func KeyStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.Accent).
		Bold(true)
}

func DescStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.TextDim)
}
