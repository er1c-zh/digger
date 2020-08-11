package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"github.com/er1c-zh/digger/util"
	"github.com/er1c-zh/go-now/log"
	"io"
	"net/http"
	"time"
)

func (d *Digger) BuildHttpsHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, __req *http.Request) {
		// https
		// try hijack c8n to client
		wHiJack, ok := w.(http.Hijacker)
		if !ok {
			log.Warn("c8n can't be hijacked")
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

		// todo cache certificate
		cert, err := util.SignHost([]string{stripPort(__req.Host)})
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
		defer func() {
			_ = tlsToClient.Close()
		}()
		tlsToClientReader := bufio.NewReader(tlsToClient)

		conn2Server, err := DefaultConnPool.GetOrCreate(ConnAction{
			URL: util.CopyAndFillURL(__req.URL, true),
			//ForceNew: true,
		})
		if err != nil {
			log.Error("GetOrCreate fail: %s", err.Error())
			return
		}
		defer DefaultConnPool.Put(conn2Server)
		serReader := bufio.NewReader(conn2Server)
		var innerErr error
		for innerErr == nil {
			func() {
				_req, err := http.ReadRequest(tlsToClientReader)
				if err != nil {
					if err != io.EOF {
						log.Error("ReadRequest fail: %s", err.Error())
					}
					innerErr = err
					return
				}
				req, reqRecord, err := wrapRequest(_req)
				if err != nil {
					log.Error("wrapRequest fail: %s", err.Error())
					innerErr = err
					return
				}
				record := _record{
					Req:            reqRecord,
					Resp:           nil,
					TimeStart:      time.Now(),
					TimeReqFinish:  time.Time{},
					TimeRespFinish: time.Time{},
					IsHttps:        true,
				}
				defer func() {
					// req never nil
					_req, err := http.NewRequest(record.Req.Method, record.Req.URL.String(), bytes.NewReader(record.Req.BodyOrigin))
					if err != nil {
						log.Error("NewRequest fail: %s", err.Error())
					}
					err = _req.ParseForm()
					if err != nil {
						log.Error("ParseForm fail: %s", err.Error())
					}
					if record.Resp != nil {
						record.Resp.Body = string(record.Resp.BodyOrigin)
					}
					record.Req.Form = _req.Form
					d.history.Add(record)
				}()
				err = req.Write(conn2Server)
				if err != nil {
					log.Error("req.Write fail: %s", err.Error())
					innerErr = err
					return
				}
				resp, err := http.ReadResponse(serReader, req)
				if err != nil {
					log.Error("ReadResponse fail: %s", err.Error())
					innerErr = err
					return
				}
				record.Resp, err = recordRespFromHttpResp(resp)
				defer func() {
					_ = resp.Body.Close()
				}()
				if err != nil {
					log.Error("ReadResponse fail: %s", err.Error())
					innerErr = err
					return
				}

				err = resp.Write(tlsToClient)
				if err != nil {
					log.Error("write to tlsToClient fail: %s", err.Error())
					innerErr = err
					return
				}
			}()
		}
		return
	}
}
