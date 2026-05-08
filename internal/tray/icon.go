package tray

import _ "embed"

//go:embed icon.ico
var trayIcon []byte

func iconData() []byte {
	return trayIcon
}
