package proxy

import (
	"github.com/er1c-zh/go-now/log"
	"net/http"
)

type noProxyHandler struct {
	router map[string]http.HandlerFunc
}

func NewNoProxyHandler() *noProxyHandler {
	return &noProxyHandler{
		router: map[string]http.HandlerFunc{},
	}
}

func (n *noProxyHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if f, ok := n.router[request.RequestURI]; ok {
		f(writer, request)
		return
	}
	log.Warn("no proxy handler uri(%s) not found.", request.RequestURI)
	writer.WriteHeader(http.StatusNotFound)
	_, err := writer.Write([]byte("not found"))
	if err != nil {
		log.Error("write to client fail: %s", err.Error())
		return
	}
	return
}

func (n *noProxyHandler) Register(uri string, handler http.HandlerFunc) {
	n.router[uri] = handler
}
