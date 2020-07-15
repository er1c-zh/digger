package main

import (
	"fmt"
	"github.com/er1c-zh/go-now/log"
	"net"
)

type Digger struct {
	Address string
	Port int16
}

func NewDigger() *Digger {
	return &Digger{
		Address: "0.0.0.0",
		Port: 8080,
	}
}

func (d *Digger) GracefullyQuit() {
	log.Info("GracefullyQuit!")
	return
}

func (d *Digger) Run() {
	log.Info("Digger running!")
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", d.Address, d.Port))
	if err != nil {
		log.Fatal("Listen fail: %s", err.Error())
		return
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Error("Accept fail: %s", err.Error())
			continue
		}
		d.Handler(conn)
	}
}

func (d *Digger) Handler(conn net.Conn) {
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Error("Read fail: %s", err.Error())
		return
	}
	log.Info("%s", string(buf[:n]))
}
