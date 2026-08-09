package layout

const (
	// AppHeaderStyle uses a bottom border → 2 visual rows.
	HeaderRows = 2
	StatusRows = 1
	// AppFooterStyle uses a top border → 2 visual rows.
	FooterRows = 2
)

func ShellRows() int {
	return HeaderRows + StatusRows + FooterRows
}

func ContentHeight(termHeight int) int {
	h := termHeight - ShellRows()
	if h < 1 {
		return 1
	}
	return h
}

func BodyHeight(contentHeight, localChrome int, floor int) int {
	h := contentHeight - localChrome
	if h < floor {
		return floor
	}
	return h
}

