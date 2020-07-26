package proxy

import (
	"bufio"
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
			d.history.Add(record)
		}()
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
		record.Resp = recordRespFromHttpReq(resp)
		defer func() {
			_ = resp.Body.Close()
		}()
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
