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

func (d *Digger) BuildHttpsHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
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

		req.URL.Scheme = "https" // todo more graceful implement force use https
		conn2Server, err := DefaultConnPool.GetOrCreate(ConnAction{
			URL:      req.URL,
			ForceNew: true,
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
				req, err := http.ReadRequest(tlsToClientReader)
				if err != nil {
					log.Error("ReadRequest fail: %s", err.Error())
					innerErr = err
					return
				}
				reqRecord, err := recordReqFromHttpReq(req)
				if err != nil {
					log.Error("recordReqFromHttpReq fail: %s", err.Error())
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
				// remove accept-encoding
				req.Header.Del("Accept-Encoding")
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
				// todo is this wrong?
				//defer func() {
				//	_ = resp.Body.Close()
				//}()
				if err != nil {
					if err == io.EOF {
						_ = tlsToClient.Close()
						return
					}
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
