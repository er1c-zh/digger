package proxy

import (
	"encoding/json"
	"fmt"
	"github.com/er1c-zh/go-now/log"
	"net/http"
	"sync/atomic"
	"time"
)

type statistics struct {
	CurrentConnCnt int64
}

func (s *statistics) BuildHandler() func(writer http.ResponseWriter, _ *http.Request) {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		j, _ := json.Marshal(s)
		_, err := writer.Write(j)
		if err != nil {
			log.Error("statistics write to writer fail: %s", err.Error())
			return
		}
		return
	}
}

func (d *Digger) GetStatisticsInfo() string {
	return fmt.Sprintf("[s]current connect: %d", d.s.CurrentConnCnt)
}

func (d *Digger) LogStatisticsInfoPerSecond() {
	go func() {
		t := time.NewTicker(time.Second)
		for {
			select {
			case <-t.C:
				log.Info("%s", d.GetStatisticsInfo())
			case <-d.done:
				t.Stop()
				return
			}
		}
	}()
}

func (d *Digger) AddCurConn() {
	atomic.AddInt64(&d.s.CurrentConnCnt, 1)
}

func (d *Digger) MinusCurConn() {
	atomic.AddInt64(&d.s.CurrentConnCnt, -1)
}
