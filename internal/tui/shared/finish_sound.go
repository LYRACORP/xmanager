package shared

import (
	"fmt"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

// PlayFinishSound rings the terminal bell and, on macOS, plays a short system sound.
func PlayFinishSound() tea.Cmd {
	return func() tea.Msg {
		fmt.Print("\a")
		if runtime.GOOS == "darwin" {
			_ = exec.Command("afplay", "/System/Library/Sounds/Glass.aiff").Start()
		}
		return nil
	}
}
