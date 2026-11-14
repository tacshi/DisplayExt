package app

import _ "embed"

var (
	//go:embed tray.png
	trayIdleIcon []byte
	//go:embed tray-active.png
	trayActiveIcon []byte
)

func trayIcon(active bool) []byte {
	if active {
		return trayActiveIcon
	}
	return trayIdleIcon
}
