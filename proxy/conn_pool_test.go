package proxy

import (
	"net/url"
	"testing"
)

func TestConnPool_GetOrCreate(t *testing.T) {
	u, err := url.Parse("https://www.baidu.com")
	if err != nil {
		t.Error(err)
		return
	}
	_, err = DefaultConnPool.GetOrCreate(ConnAction{URL: u})
	if err != nil {
		t.Error(err)
		return
	}
}
