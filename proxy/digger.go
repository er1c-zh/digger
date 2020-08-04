package proxy

import (
	"github.com/er1c-zh/go-now/log"
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
		d.noProxyHandler.Register("/history/clean", d.history.BuildCleanHandler())

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
		d.BuildHttpsHandler()(w, req)
		return
	} else {
		if !req.URL.IsAbs() {
			d.noProxyHandler.ServeHTTP(w, req)
			return
		} else {
			d.BuildHttpHandler()(w, req)
			return
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
