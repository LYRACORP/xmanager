package layout

import (
	"github.com/charmbracelet/bubbles/table"
)

const (
	BreakpointNarrow = iota
	BreakpointMedium
	BreakpointWide
)

const (
	narrowMax  = 79
	mediumMax  = 119
)

func Breakpoint(width int) int {
	switch {
	case width <= narrowMax:
		return BreakpointNarrow
	case width <= mediumMax:
		return BreakpointMedium
	default:
		return BreakpointWide
	}
}

func Clamp(min, val, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func SplitHorizontal(width, gap, leftPct, minLeft, minRight int) (leftW, rightW int, stack bool) {
	if width < minLeft+minRight+gap {
		return width - 2, width - 2, true
	}
	leftW = (width * leftPct) / 100
	if leftW < minLeft {
		leftW = minLeft
	}
	rightW = width - leftW - gap
	if rightW < minRight {
		rightW = minRight
		leftW = width - rightW - gap
		if leftW < minLeft {
			leftW = minLeft
		}
	}
	return leftW, rightW, false
}

func TableHeight(totalHeight, reserved, floor int) int {
	return BodyHeight(totalHeight, reserved, floor)
}

func GaugeWidth(totalWidth, count, gap, minEach int) int {
	if count < 1 {
		count = 1
	}
	gaps := (count - 1) * gap
	available := totalWidth - gaps - 4
	w := available / count
	if w < minEach {
		return minEach
	}
	return w
}

// AdaptiveColumns drops optional columns (marked with Width <= 0) on narrow terminals
// and scales fixed-width columns proportionally when space is tight.
func AdaptiveColumns(width int, cols []table.Column) []table.Column {
	bp := Breakpoint(width)
	if bp == BreakpointWide {
		return cols
	}

	var required []table.Column
	var optional []table.Column
	for _, c := range cols {
		if c.Width <= 0 {
			optional = append(optional, c)
		} else {
			required = append(required, c)
		}
	}

	out := required
	if bp == BreakpointMedium && len(optional) > 0 {
		out = append(out, optional[0])
	}

	totalFixed := 0
	for _, c := range out {
		if c.Width > 0 {
			totalFixed += c.Width
		}
	}
	if totalFixed > width-4 && totalFixed > 0 {
		scale := float64(width-4) / float64(totalFixed)
		for i := range out {
			if out[i].Width > 0 {
				w := int(float64(out[i].Width) * scale)
				if w < 6 {
					w = 6
				}
				out[i].Width = w
			}
		}
	}
	return out
}

func InputWidth(parentWidth int) int {
	return Clamp(30, parentWidth-6, 72)
}

func PanelWidth(parentWidth int) int {
	return Clamp(20, parentWidth-2, parentWidth)
}
