package proxy

import (
	"encoding/json"
	"github.com/er1c-zh/go-now/log"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type _recordReq struct {
	Method        string
	URL           *url.URL
	Proto         string
	ProtoMajor    int
	ProtoMinor    int
	Header        http.Header
	ContentLength int64
	Host          string
	RemoteAddr    string
	RequestURI    string
	BodyOrigin    []byte `json:"body_origin;omitempty"`
	// todo parse body
}

type teeReadCloser struct {
	originReader io.ReadCloser
	tee          io.Reader
}

func TeeReadCloser(r io.ReadCloser, w io.Writer) io.ReadCloser {
	return &teeReadCloser{
		tee:          io.TeeReader(r, w),
		originReader: r,
	}
}

func (t *teeReadCloser) Read(p []byte) (n int, err error) {
	return t.tee.Read(p)
}

func (t *teeReadCloser) Close() error {
	return t.originReader.Close()
}

func recordReqFromHttpReq(src *http.Request) (*_recordReq, error) {
	r := &_recordReq{
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
	src.Body = TeeReadCloser(src.Body, r)
	return r, nil
}

func (r *_recordReq) Write(p []byte) (n int, err error) {
	r.BodyOrigin = append(r.BodyOrigin, p...)
	return len(p), nil
}

type _recordResp struct {
	Status        string
	StatusCode    int
	Proto         string
	ProtoMajor    int
	ProtoMinor    int
	Header        http.Header
	ContentLength int64
	BodyOrigin    []byte
}

func recordRespFromHttpResp(src *http.Response) (*_recordResp, error) {
	r := &_recordResp{
		Status:        src.Status,
		StatusCode:    src.StatusCode,
		Proto:         src.Proto,
		ProtoMajor:    src.ProtoMajor,
		ProtoMinor:    src.ProtoMinor,
		Header:        src.Header,
		ContentLength: src.ContentLength,
		BodyOrigin:    nil,
	}
	src.Body = TeeReadCloser(src.Body, r)
	return r, nil
}

func (r *_recordResp) Write(p []byte) (n int, err error) {
	r.BodyOrigin = append(r.BodyOrigin, p...)
	return len(p), nil
}

type _record struct {
	Req  *_recordReq
	Resp *_recordResp

	TimeStart      time.Time
	TimeReqFinish  time.Time
	TimeRespFinish time.Time

	IsHttps bool
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
		log.Debug("==%s", string(j))
		_, err := writer.Write(j)
		if err != nil {
			log.Error("statistics write to writer fail: %s", err.Error())
			return
		}
		return
	}
}

func (l *_recordList) BuildCleanHandler() func(writer http.ResponseWriter, _ *http.Request) {
	return func(writer http.ResponseWriter, _ *http.Request) {
		l.data = l.data[0:0]
		writer.WriteHeader(http.StatusOK)
		return
	}
}

func (l *_recordList) Add(r _record) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.data = append(l.data, r)
}
