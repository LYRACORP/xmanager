package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	PadXS = 0
	PadSM = 1
	PadMD = 2
	PadLG = 3
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
	Selection  lipgloss.Color
}

// Dark is the minimal dark palette (config key remains "dark").
var Dark = Theme{
	Name:       "dark",
	Primary:    lipgloss.Color("#D4A574"),
	Secondary:  lipgloss.Color("#A0A0A0"),
	Accent:     lipgloss.Color("#D4A574"),
	Success:    lipgloss.Color("#6A9955"),
	Warning:    lipgloss.Color("#CCA700"),
	Error:      lipgloss.Color("#F14C4C"),
	Critical:   lipgloss.Color("#C42B1C"),
	Muted:      lipgloss.Color("#858585"),
	Text:       lipgloss.Color("#E8E8E8"),
	TextDim:    lipgloss.Color("#858585"),
	Background: lipgloss.Color("#1E1E1E"),
	Surface:    lipgloss.Color("#252526"),
	Border:     lipgloss.Color("#3C3C3C"),
	Selection:  lipgloss.Color("#2A2D2E"),
}

var Light = Theme{
	Name:       "light",
	Primary:    lipgloss.Color("#7C3AED"),
	Secondary:  lipgloss.Color("#6B7280"),
	Accent:     lipgloss.Color("#7C3AED"),
	Success:    lipgloss.Color("#059669"),
	Warning:    lipgloss.Color("#D97706"),
	Error:      lipgloss.Color("#DC2626"),
	Critical:   lipgloss.Color("#B91C1C"),
	Muted:      lipgloss.Color("#9CA3AF"),
	Text:       lipgloss.Color("#1A1A1A"),
	TextDim:    lipgloss.Color("#6B7280"),
	Background: lipgloss.Color("#FAFAFA"),
	Surface:    lipgloss.Color("#F0F0F0"),
	Border:     lipgloss.Color("#E5E7EB"),
	Selection:  lipgloss.Color("#E8E4F0"),
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

func GaugeFillChar() string  { return "▓" }
func GaugeEmptyChar() string { return "░" }

func EmptyStateText() string {
	return MutedText().Render("— no data —")
}

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(Current.Text).
		PaddingLeft(PadSM)
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
		Padding(PadXS, PadMD)
}

func ActivePanelStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(Current.Accent)
}

func AppHeaderStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(Current.Surface).
		Foreground(Current.Text).
		Width(width).
		Padding(PadXS, PadMD).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(Current.Border)
}

func AppFooterStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(Current.Background).
		Width(width).
		Padding(PadXS, PadMD).
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
	return lipgloss.NewStyle().Foreground(Current.Border).Render(line)
}

func ScreenChrome(title, subtitle string, width int) string {
	header := HeaderStyle().Render(title)
	var parts []string
	parts = append(parts, header)
	if subtitle != "" {
		parts = append(parts, SubtitleStyle().PaddingLeft(PadSM).Render(subtitle))
	}
	return strings.Join(parts, "\n")
}

func SelectedRowStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.Text).
		Background(Current.Selection)
}

func InputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(Current.Text).
		Background(Current.Surface)
}

func InputFocusedStyle() lipgloss.Style {
	return InputStyle().
		Bold(true).
		Foreground(Current.Accent)
}

func ViewportStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(Current.Border).
		Padding(PadXS, PadMD)
}

func MessageStyle(role string) lipgloss.Style {
	switch role {
	case "user":
		return lipgloss.NewStyle().Foreground(Current.Accent).Bold(true)
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
		Padding(PadXS, PadSM).
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
