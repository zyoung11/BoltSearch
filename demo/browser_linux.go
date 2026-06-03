//go:build linux

package main

import (
	"os"
	"os/exec"
	"os/user"
)

func openBrowser(url string) error {
	currentUser, _ := user.Current()
	if currentUser != nil && currentUser.Uid == "0" {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			return exec.Command("sudo", "-u", sudoUser, "xdg-open", url).Start()
		}

		env := os.Environ()
		env = append(env, "DISPLAY=:0")

		if xdgCurrentDesktop := os.Getenv("XDG_CURRENT_DESKTOP"); xdgCurrentDesktop != "" {
			env = append(env, "XDG_CURRENT_DESKTOP="+xdgCurrentDesktop)
		}
		if xdgSessionType := os.Getenv("XDG_SESSION_TYPE"); xdgSessionType != "" {
			env = append(env, "XDG_SESSION_TYPE="+xdgSessionType)
		}

		command := exec.Command("xdg-open", url)
		command.Env = env
		return command.Start()
	}

	return exec.Command("xdg-open", url).Start()
}
