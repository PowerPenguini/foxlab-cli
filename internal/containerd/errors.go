package containerd

import (
	"fmt"
	"strings"
)

const containerdAccessHint = "run with sudo or grant access to the containerd socket"

func WithAccessHint(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "permission denied") || !strings.Contains(message, "containerd") {
		return err
	}
	if strings.Contains(message, containerdAccessHint) {
		return err
	}
	return fmt.Errorf("%w; %s", err, containerdAccessHint)
}
