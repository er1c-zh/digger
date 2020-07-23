package proxy

import (
	"encoding/json"
	"github.com/er1c-zh/go-now/log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type _recordReq struct {
	Method        string
	URL           *url.URL
	Proto         string // "HTTP/1.0"
	ProtoMajor    int    // 1
	ProtoMinor    int    // 0
	Header        http.Header
	ContentLength int64
	Host          string
	RemoteAddr    string
	RequestURI    string
}

func recordReqFromHttpReq(src *http.Request) *_recordReq {
	return &_recordReq{
		Method:        src.Method,
		URL:           src.URL,
		Proto:         src.Proto,
		ProtoMajor:    src.ProtoMajor,
		ProtoMinor:    src.ProtoMinor,
		Header:        src.Header,
		ContentLength: src.ContentLength,
		Host:          src.Host,
		RemoteAddr:    src.RemoteAddr,
		RequestURI:    src.RequestURI,
	}
}

type _recordResp struct {
}

type _record struct {
	Req  *_recordReq
	Resp *_recordResp

	TimeStart      time.Time
	TimeReqFinish  time.Time
	TimeRespFinish time.Time
}

type _recordList struct {
	mtx  sync.Mutex
	data []_record
}

func newRecordList() _recordList {
	return _recordList{
		data: make([]_record, 0),
	}
}

func (l *_recordList) BuildHandler() func(writer http.ResponseWriter, _ *http.Request) {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Add("content-type", "application/json")
		writer.Header().Add("content-type", "charset=utf8")
		writer.WriteHeader(http.StatusOK)
		j, _ := json.Marshal(l.data)
		_, err := writer.Write(j)
		if err != nil {
			log.Error("statistics write to writer fail: %s", err.Error())
			return
		}
		return
	}
}

func (l *_recordList) Add(r _record) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.data = append(l.data, r)
}
