package monitor

import "os"

func pid() int {
	return os.Getpid()
}
