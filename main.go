package main

import (
	boot "github.com/er1c-zh/go-now/go_boot"
	"github.com/er1c-zh/go-now/log"
)

func main() {
	digger := NewDigger()
	boot.RegisterExitHandlers(func() {
		digger.GracefullyQuit()
	})
	boot.RegisterExitHandlers(func() {
		log.Flush()
	})

	go func() {
		digger.Run()
		boot.Exit()
	}()

	boot.WaitExit(0)
}