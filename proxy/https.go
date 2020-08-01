package proxy

import (
	"bufio"
	"crypto/tls"
	"github.com/er1c-zh/digger/util"
	"github.com/er1c-zh/go-now/log"
	"io"
	"net"
	"net/http"
	"time"
)

func (d *Digger) BuildHttpsHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
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
			Certificates:       []tls.Certificate{*cert},
			InsecureSkipVerify: true,
		})
		if err := tlsToClient.Handshake(); err != nil {
			log.Error("shake hand with client fail: %s", err.Error())
			return
		}
		defer tlsToClient.Close()
		tlsToClientReader := bufio.NewReader(tlsToClient)
		for _, err := tlsToClientReader.Peek(1); err != io.EOF; {
			req, err = http.ReadRequest(tlsToClientReader)
			if err != nil {
				log.Error("ReadRequest fail: %s", err.Error())
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
				InsecureSkipVerify: true,
			})
			defer tlsToServer.Close()
			if err := tlsToServer.Handshake(); err != nil {
				log.Error("shake hand with server fail: %s", err.Error())
				return
			}
			reqRecord, err := recordReqFromHttpReq(req)
			if err != nil {
				log.Error("recordReqFromHttpReq fail: %s", err.Error())
				return
			}
			record := _record{
				Req:            reqRecord,
				Resp:           nil,
				TimeStart:      time.Now(),
				TimeReqFinish:  time.Time{},
				TimeRespFinish: time.Time{},
			}
			defer func() {
				d.history.Add(record)
			}()
			err = req.Write(tlsToServer)
			if err != nil {
				log.Error("write fail: %s", err.Error())
				return
			}
			resp, err := http.ReadResponse(bufio.NewReader(tlsToServer), req)
			if err != nil {
				log.Error("ReadResponse fail: %s", err.Error())
				return
			}

			err = resp.Write(tlsToClient)
			if err != nil {
				log.Error("write to tlsToClient fail: %s", err.Error())
				return
			}
			/*
				for k, v := range resp.Header {
					for _, _v := range v {
						w.Header().Add(k, _v)
					}
				}
				w.WriteHeader(resp.StatusCode)
				n, err := io.Copy(w, resp.Body)
				if err != nil {
					log.Error("io.Copy fail: %s", err.Error())
					return
				}
				log.Info("io.Copy cnt: %d", n)
			*/
		}
		return
	}
}
