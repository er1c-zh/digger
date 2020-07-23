package proxy

import (
	"github.com/er1c-zh/go-now/log"
	"io"
	"net"
	"net/http"
	"strconv"
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
		// https
		// try hijack connection to client
		wHiJack, ok := w.(http.Hijacker)
		if !ok {
			log.Warn("connection can't be hijacked")
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
		/*
			tlsToClient := tls.Server(connToClient, &tls.Config{InsecureSkipVerify: true})
			defer tlsToClient.Close()
			if err := tlsToClient.Handshake(); err != nil {
				log.Error("shake hand with client fail: %s", err.Error())
				return
			}

		*/

		addr := req.URL.Host
		if req.URL.Port() == "" {
			addr += ":443"
		}
		connToServer, err := net.Dial("tcp", addr)
		if err != nil {
			log.Error("dial %s fail: %s", err.Error())
			return
		}
		defer connToServer.Close()
		/*
			tlsToServer := tls.Client(connToServer, &tls.Config{InsecureSkipVerify: true})
			defer tlsToServer.Close()
			if err := tlsToServer.Handshake(); err != nil {
				log.Error("shake hand with server fail: %s", err.Error())
				return
			}
			_, err = io.Copy(tlsToServer, tlsToClient)
			if err != nil {
				log.Error("io.Copy client to server fail: %s", err.Error())
				return
			}
			_, err = io.Copy(tlsToClient, tlsToServer)
			if err != nil {
				log.Error("io.Copy server to client fail: %s", err.Error())
				return
			}
		*/
		var wg sync.WaitGroup
		var errClient, errServer error
		wg.Add(2)
		go func() {
			_, errClient = io.Copy(connToServer, connToClient)
			wg.Done()
		}()
		go func() {
			_, errServer = io.Copy(connToClient, connToServer)
			wg.Done()
		}()
		wg.Wait()
		return
	} else {
		if !req.URL.IsAbs() {
			d.noProxyHandler.ServeHTTP(w, req)
			return
		} else {
			d.BuildHttpHandler()(w, req)
		}
	}
}

var (
	cert = []byte(`-----BEGIN CERTIFICATE-----
MIIDkzCCAnsCFFFzcXTvNOKmASnbOcPi8OQHYsauMA0GCSqGSIb3DQEBCwUAMIGF
MQswCQYDVQQGEwJDTjERMA8GA1UECAwIU2hhbmRvbmcxDzANBgNVBAcMBllhbnRh
aTENMAsGA1UECgwEZXIxYzENMAsGA1UECwwEZXIxYzENMAsGA1UEAwwEZXIxYzEl
MCMGCSqGSIb3DQEJARYWZXJpY3poYW85NkBob3RtYWlsLmNvbTAeFw0yMDA3MjMx
NzU3MzlaFw0zMDA3MjExNzU3MzlaMIGFMQswCQYDVQQGEwJDTjERMA8GA1UECAwI
U2hhbmRvbmcxDzANBgNVBAcMBllhbnRhaTENMAsGA1UECgwEZXIxYzENMAsGA1UE
CwwEZXIxYzENMAsGA1UEAwwEZXIxYzElMCMGCSqGSIb3DQEJARYWZXJpY3poYW85
NkBob3RtYWlsLmNvbTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAOfQ
UIoaXFOjAEZGCHzcoHuquBBtkmuku0QYwH8ip0UUsrwrgu1IsUURWuTcg5cQrI+s
OslilztBlJy/KWq2CgeMH8CgAZw0drbnUpNNb9SQU7bdw0iypOim7avIqAHK9XWj
EZ+TyYwMdKHoCnTy7FpPGCIA4r5/KCA3kE3KRkY2plJ0iftTW1bOj0jeLTYz8KwF
nkuW81RnmOY757lbFdnoQsBVmUw4exZcZ7co9MwIJRQzhC5Pkh6zF9GnWpG+88F7
GukgjDCnWcQubrEBkeDe7YoBgt16Ltf/NGmpsOdB7YX3dr1+86hWDtd4K9SyllMh
G/tDy+tdslsI9m2d8eMCAwEAATANBgkqhkiG9w0BAQsFAAOCAQEANhTYxL5lkJ5I
zYPFAHycmP/51uvShA/1RP/mhrWN+aghC18rthEbATS4MHmhd701q4qsoZFIPLoS
p2LN6huPZ16+vU+VSXGPkRjLuZSKxJ6HEpErNEhsiErojyKdkTn27tqGm2uoJr1C
VzOqbPdlVL2+76gvT30ZK6EJ5Pp5X1yztYLS/1MNw7Zr/5SAc8tUI/vj3YtNYlWl
EM35+ZtFBaZ6QiBPv1A9JIHXhlI4SDWpLuNfNm5feU2PNBXgGYZXjKk4xYnhAODG
5dXnqBMtYM8E8ndt+451qbPKBnFCLRk+1kBnmHrezw9h7w4Op7YvNLeTvGFnl1UK
Oefg+GNGOQ==
-----END CERTIFICATE-----`)
	certPrivate = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA59BQihpcU6MARkYIfNyge6q4EG2Sa6S7RBjAfyKnRRSyvCuC
7UixRRFa5NyDlxCsj6w6yWKXO0GUnL8parYKB4wfwKABnDR2tudSk01v1JBTtt3D
SLKk6Kbtq8ioAcr1daMRn5PJjAx0oegKdPLsWk8YIgDivn8oIDeQTcpGRjamUnSJ
+1NbVs6PSN4tNjPwrAWeS5bzVGeY5jvnuVsV2ehCwFWZTDh7Flxntyj0zAglFDOE
Lk+SHrMX0adakb7zwXsa6SCMMKdZxC5usQGR4N7tigGC3Xou1/80aamw50Hthfd2
vX7zqFYO13gr1LKWUyEb+0PL612yWwj2bZ3x4wIDAQABAoIBAQDVJtn3srd0fCwT
ce/6B9BVBixLhsUcz5MV0YCnJlESFy8mEQhJcQ73SDcAu7cP39gcH6zKYipW5T1m
R+woYAym1fSYZUg1vpPuKJPoOEr89FzVh+I55XH3Lw7ZZx78zweWzIO27OhlK0rP
WRLMaFZlz9aL5a6YpUlbHlxE+xpVEckpCIzfvJSdWIRS4iuN5NtLWXsnOMrCr/v+
yG4DSFmJY8QGqwTsVqgPxVeH+eZPOHOm9boRVDB9zRKH58rbDDNKMMFQ6EOwqM0F
kXHzuS3B12tZNfGtyU0TsL2lim+rMvlAWR8JqzaNvSVHMlTwepX1V7EtA+9Fd+SM
zUDuuuBRAoGBAPVip2pMJUH5P4Cpul1dj5shBwIKrDIVa2RDgZREVBmjKCtXIOei
7yMMhDQOapon6gFUHQcIyZEZCHgn0ja9kxcyK74jLt3tzp1TyqEFJVTbiEtqChnw
q9cJIzzbw74vooQsIbIODlM48WqjRlliLoOdzp7kmsNBhXWK6z/uZt/rAoGBAPHX
XwUHqvowNo5YjvQc3bavdBZsmNu/maHMTZPZHxyG1ObMD5rUesdCKCN+Utb45lpe
FsyBuIQE7vRRLy1S/OwCQMWfyM1XV5AVBr+4NZAdhLIR5BXYKPC/P3iKcbD7fS+s
AqhB4M2qcKE2MnnHrD8PRlqFsu05AxseHRXUoC/pAoGATNL0Ix1v1LXaIcgBptVx
7llqvkLlIlD+bEeOPAMgaV5hZyBCFwM15z017q5MxbKVWpEg/WDM6nZx5lxhPe4g
LPTyKPcO50BanXrsR3k69NQ+WY37V5+3zPz5YUZUhCiZstO2QO6RoZCEVKSFk9pf
QamYVLqxkUvkIqa5fCyBXL0CgYB9P1IZk8gLvG50uA6JBG4az7Eqb+GWZRtWvS0s
NdUz++xE/0fRottXWL7a6vBSHyOFh5b9IO2Did6LL4RkT8dnHx+WedMP7X0OxKTz
I56x3We8pSFf4swJKrLfZavNweEqkEXsB/o56VxdUWlAwpVFL077UKTC0LT4FVdw
1+aCCQKBgQDjFIeoAfOV6H3g/Y6KNkDSrCpnoAbZ3aPpMAuR5xQy+/4SKMnj14V4
T91RMnH/6Kv+8nG6B9S8fKKO2B9vK9phl1EQACtKV2YUG3J9PTgdIn1xViZQ7nPD
4m1dBt/CJ9+MEHKjT/JqOMceAuN7JpiHZfUz5xHN79UQg/xDUQ9TYg==
-----END RSA PRIVATE KEY-----`)
)
