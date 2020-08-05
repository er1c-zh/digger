package proxy

import (
	"bufio"
	"bytes"
	"github.com/er1c-zh/go-now/log"
	"io"
	"net"
	"net/http"
	"time"
)

func (d *Digger) BuildHttpHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
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
		log.Info("[is abs: %t][host(%s)][port(%s)]connect to (%s)", req.URL.IsAbs(), req.URL.Host, req.URL.Port(), req.URL.Host)
		addr := req.URL.Host
		if req.URL.Port() == "" {
			addr += ":80"
		}
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			log.Error("dial (%s) fail: %s", addr, err.Error())
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		err = req.Write(conn)
		if err != nil {
			log.Error("write fail: %s", err.Error())
			return
		}
		record.TimeReqFinish = time.Now()
		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		if err != nil {
			log.Error("ReadResponse fail: %s", err.Error())
			return
		}
		record.Resp, err = recordRespFromHttpResp(resp)
		defer func() {
			_ = resp.Body.Close()
		}()
		if err != nil {
			log.Error("recordRespFromHttpResp fail: %s", err.Error())
			return
		}
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
		return
	}
}
