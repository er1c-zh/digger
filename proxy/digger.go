package proxy

import (
	"crypto/tls"
	"github.com/er1c-zh/digger/util"
	"github.com/er1c-zh/go-now/log"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type Digger struct {
	Address     string
	Port        int16
	HistorySize int64

	done     chan struct{}
	initOnce sync.Once

	s statistics

	noProxyHandler *noProxyHandler

	history _recordList
	running []_record
}

func NewDigger() *Digger {
	return &Digger{
		Address: "0.0.0.0",
		Port:    8080,
		done:    make(chan struct{}),
		s: statistics{
			CurrentConnCnt: 0,
		},
		noProxyHandler: NewNoProxyHandler(),
		history:        newRecordList(),
	}
}

func (d *Digger) GracefullyQuit() {
	close(d.done)
	log.Info("GracefullyQuit!")
	return
}

func (d *Digger) Run() {
	d.initOnce.Do(func() {
		d.LogStatisticsInfoPerSecond()
		d.noProxyHandler.Register("/statistics", d.s.BuildHandler())
		d.noProxyHandler.Register("/history", d.history.BuildHandler())

		log.Info("Digger running!")

		err := http.ListenAndServe(d.Address+":"+strconv.FormatInt(int64(d.Port), 10), d)
		if err != nil {
			log.Fatal("ListenAndServe fail: %s", err.Error())
			return
		}
		return
	})
}

func (d *Digger) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	d.AddCurConn()
	defer func() {
		d.MinusCurConn()
	}()
	if req.Method == "CONNECT" {
		// https
		// try hijack connection to client
		wHiJack, ok := w.(http.Hijacker)
		if !ok {
			log.Warn("connection can't be hijacked")
			return
		}
		connToClient, _, err := wHiJack.Hijack()
		if err != nil {
			log.Error("hijack fail: %s", err.Error())
			return
		}
		defer func() {
			_ = connToClient.Close()
		}()
		_, err = connToClient.Write([]byte("HTTP/1.1 200 Connection established!\r\n\r\n"))
		if err != nil {
			log.Error("write response to client fail: %s", err.Error())
			return
		}

		cert, err := util.SignHost([]string{stripPort(req.Host)})
		if err != nil {
			log.Error("gen cert fail: %s", err.Error())
			return
		}

		tlsToClient := tls.Server(connToClient, &tls.Config{
			Certificates: []tls.Certificate{*cert},
			InsecureSkipVerify: true,
		})
		defer tlsToClient.Close()
		if err := tlsToClient.Handshake(); err != nil {
			log.Error("shake hand with client fail: %s", err.Error())
			return
		}

		addr := req.URL.Host
		if req.URL.Port() == "" {
			addr += ":443"
		}
		connToServer, err := net.Dial("tcp", addr)
		if err != nil {
			log.Error("dial %s fail: %s", err.Error())
			return
		}
		defer connToServer.Close()
		tlsToServer := tls.Client(connToServer, &tls.Config{
			InsecureSkipVerify:          true,
		})
		defer tlsToServer.Close()
		if err := tlsToServer.Handshake(); err != nil {
			log.Error("shake hand with server fail: %s", err.Error())
			return
		}
		var wg sync.WaitGroup
		var errClient, errServer error
		wg.Add(2)
		go func() {
			_, errClient = io.Copy(tlsToServer, tlsToClient)
			wg.Done()
		}()
		go func() {
			_, errServer = io.Copy(tlsToClient, tlsToServer)
			wg.Done()
		}()
		wg.Wait()
		return
	} else {
		if !req.URL.IsAbs() {
			d.noProxyHandler.ServeHTTP(w, req)
			return
		} else {
			d.BuildHttpHandler()(w, req)
		}
	}
}

func stripPort(s string) string {
	ix := strings.IndexRune(s, ':')
	if ix == -1 {
		return s
	}
	return s[:ix]
}
