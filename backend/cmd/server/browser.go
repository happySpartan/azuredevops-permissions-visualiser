package main

import (
	"log"
	"os/exec"
	"runtime"
)

func browserCommand(goos, url string) ([]string, bool) {
	switch goos {
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}, true
	case "linux":
		return []string{"xdg-open", url}, true
	default:
		return nil, false
	}
}

func openBrowser(url string) {
	command, ok := browserCommand(runtime.GOOS, url)
	if !ok {
		return
	}
	if err := exec.Command(command[0], command[1:]...).Start(); err != nil {
		log.Printf("Open %s in a browser (automatic browser launch failed: %v)", url, err)
	}
}
