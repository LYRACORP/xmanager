package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

// Dark is the cyberpunk palette (config key remains "dark").
var Dark = Theme{
	Name:       "dark",
	Primary:    lipgloss.Color("#FF00FF"),
	Secondary:  lipgloss.Color("#00FFFF"),
	Accent:     lipgloss.Color("#FFE600"),
	Success:    lipgloss.Color("#39FF14"),
	Warning:    lipgloss.Color("#FF6B00"),
	Error:      lipgloss.Color("#FF0040"),
	Critical:   lipgloss.Color("#CC0022"),
	Muted:      lipgloss.Color("#6B7280"),
	Text:       lipgloss.Color("#E0E0FF"),
	TextDim:    lipgloss.Color("#6B7280"),
	Background: lipgloss.Color("#0A0A0F"),
	Surface:    lipgloss.Color("#12121A"),
	Border:     lipgloss.Color("#2D2D44"),
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

func GaugeFillChar() string { return "▓" }
func GaugeEmptyChar() string { return "░" }

func EmptyStateText() string {
	return MutedText().Render("— no data —")
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
	border := lipgloss.NormalBorder()
	if Current.Name == "light" {
		border = lipgloss.RoundedBorder()
	}
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(Current.Border).
		Padding(0, 1)
}

func ActivePanelStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(Current.Primary)
}

func AppHeaderStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(Current.Surface).
		Foreground(Current.Primary).
		Bold(true).
		Width(width).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(Current.Primary)
}

func AppFooterStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(Current.Background).
		Width(width).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(Current.Border)
}

func BackgroundStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(Current.Background).
		Width(width)
}

func Divider(width int) string {
	if width < 1 {
		width = 1
	}
	line := strings.Repeat("─", width)
	return lipgloss.NewStyle().Foreground(Current.Secondary).Render(line)
}

func ScreenChrome(title, subtitle string, width int) string {
	header := HeaderStyle().Render(title)
	var parts []string
	parts = append(parts, header)
	if subtitle != "" {
		parts = append(parts, SubtitleStyle().PaddingLeft(1).Render(subtitle))
	}
	divW := width - 2
	if divW < 10 {
		divW = 10
	}
	parts = append(parts, " "+Divider(divW))
	return strings.Join(parts, "\n")
}

func InputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.Text).
		Background(Current.Surface)
}

func InputFocusedStyle() lipgloss.Style {
	return InputStyle().
		Bold(true).
		Foreground(Current.Secondary)
}

func ViewportStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(Current.Border).
		Padding(0, 1)
}

func MessageStyle(role string) lipgloss.Style {
	switch role {
	case "user":
		return lipgloss.NewStyle().Foreground(Current.Secondary).Bold(true)
	case "assistant":
		return lipgloss.NewStyle().Foreground(Current.Text)
	case "system":
		return lipgloss.NewStyle().Foreground(Current.Muted).Italic(true)
	default:
		return lipgloss.NewStyle().Foreground(Current.TextDim)
	}
}

func LogLevelStyle(level string) lipgloss.Style {
	switch strings.ToLower(level) {
	case "error", "fatal", "critical":
		return lipgloss.NewStyle().Foreground(Current.Error).Bold(true)
	case "warn", "warning":
		return lipgloss.NewStyle().Foreground(Current.Warning).Bold(true)
	case "info":
		return lipgloss.NewStyle().Foreground(Current.Secondary)
	case "debug", "trace":
		return lipgloss.NewStyle().Foreground(Current.Muted)
	default:
		return lipgloss.NewStyle().Foreground(Current.TextDim)
	}
}

func BadgeStyle(color lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.Text).
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
