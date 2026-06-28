package topologyui

import "time"

const spinnerInterval = 120 * time.Millisecond

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinner(frame int) string {
	if len(spinnerFrames) == 0 {
		return "-"
	}
	if frame < 0 {
		frame = -frame
	}
	return spinnerFrames[frame%len(spinnerFrames)]
}

func animatedState(state string) bool {
	switch state {
	case "starting", "stopping", "loading", "pulling", "creating", "applying", "refreshing":
		return true
	default:
		return false
	}
}
