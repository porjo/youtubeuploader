// +build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func SetSignalNotify(c chan os.Signal) {
	signal.Notify(c, syscall.SIGUSR1)
}
