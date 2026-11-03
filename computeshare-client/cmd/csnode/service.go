//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

const unit = `[Unit]
Description=ComputeShare node agent (csnode)
After=network-online.target incus.service
Wants=network-online.target

[Service]
ExecStart=%s run
Restart=always
RestartSec=5
Environment=CSNODE_DIR=%s

[Install]
WantedBy=multi-user.target
`

// cmdService implements 006 R6 (systemd). Windows service: see card 003 installer.
func cmdService(args []string) {
	if len(args) != 1 {
		usage()
	}
	if runtime.GOOS != "linux" {
		die(exitErr, "on Windows the service is installed by install-windows.ps1 (card 003)")
	}
	const path = "/etc/systemd/system/csnode.service"
	switch args[0] {
	case "install":
		exe, _ := os.Executable()
		dir := os.Getenv("CSNODE_DIR")
		if dir == "" {
			dir = "/etc/csnode"
		}
		if err := os.WriteFile(path, []byte(fmt.Sprintf(unit, exe, dir)), 0o644); err != nil {
			die(exitErr, "%v", err)
		}
		exec.Command("systemctl", "daemon-reload").Run()
		if err := exec.Command("systemctl", "enable", "--now", "csnode").Run(); err != nil {
			die(exitErr, "unit written to %s but systemctl failed: %v", path, err)
		}
		fmt.Println("csnode service installed and started")
	case "uninstall":
		exec.Command("systemctl", "disable", "--now", "csnode").Run()
		os.Remove(path)
		exec.Command("systemctl", "daemon-reload").Run()
		fmt.Println("csnode service removed")
	default:
		usage()
	}
}
