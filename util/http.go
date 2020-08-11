package util

import (
	"net/http"
	"net/url"
)

// this will read src.Body
func WrapProxyRequest(src *http.Request) *http.Request {
	// disable content encode
	src.Header.Del("Accept-Encoding")
	return src
}

func CopyAndFillURL(src *url.URL, isHTTPS ...bool) *url.URL {
	if src == nil {
		return nil
	}
	dest, _ := url.Parse(src.String())
	if !dest.IsAbs() {
		scheme := "http"
		if (len(isHTTPS) > 0 && isHTTPS[0]) ||
			(len(isHTTPS) == 0 && dest.Port() == "443") {
			scheme = "https"
		}
		dest.Scheme = scheme
	}
	return dest
}
