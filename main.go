package main

import (
	"github.com/er1c-zh/digger/proxy"
	boot "github.com/er1c-zh/go-now/go_boot"
	"github.com/er1c-zh/go-now/log"
)

func main() {
	digger := proxy.NewDigger()
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
