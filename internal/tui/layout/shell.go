package layout

const (
	HeaderRows = 1
	StatusRows = 1
	FooterRows = 1
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

