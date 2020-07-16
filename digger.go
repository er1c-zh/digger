package main

import (
	"bytes"
	"fmt"
	"github.com/er1c-zh/go-now/log"
	"io"
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

	// request line
	// method SPACE request-target SP HTTP-version CRLF
	var method, target, version string
	requestLine := string(buf[:bytes.IndexByte(buf, '\n')])
	requestLineMatchedCnt, err := fmt.Sscanf(requestLine,
		"%s %s %s", &method, &target, &version)
	if err != nil {
		log.Error("parse request line(%s) fail: %s", requestLine, err.Error())
		return
	}
	if requestLineMatchedCnt != 3 {
		log.Error("invalid request line(%s) : %s %s %s", requestLine, method, target, version)
		return
	}

	log.Info("[%s][%s]%s", method, version, target)


	conn2Server, err := net.Dial("tcp", target)
	if err != nil {
		log.Error("conn to %s fail: %s", target, err.Error())
		return
	}

	if method == "CONNECT" {
		_, _ = conn.Write([]byte(version + " 200 \n\n"))
	} else {
		_, _ = conn2Server.Write(buf[:n])
	}

	go io.Copy(conn, conn2Server)
	go io.Copy(conn2Server, conn)
}
