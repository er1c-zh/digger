package proxy

import (
	"bufio"
	"crypto/tls"
	"github.com/er1c-zh/go-now/log"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var (
	DefaultConnPool ConnPool
)

func init() {
	DefaultConnPool = NewConnPool()
}

type ConnAction struct {
	URL *url.URL
	ForceNew bool
}

func (a ConnAction) GetKey() string {
	return a.URL.Scheme + ":" + a.URL.Host
}

type ConnPool interface {
	GetOrCreate(action ConnAction) (net.Conn, error)
	Put(net.Conn)
}

type connPool struct {
	idle    sync.Map
	running sync.Map
	mtx     sync.Mutex
}

func NewConnPool() ConnPool {
	return &connPool{}
}

func (c *connPool) GetOrCreate(action ConnAction) (net.Conn, error) {
	if action.ForceNew {
		return c.getNew(action)
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	connListUntyped, ok := c.idle.Load(action.GetKey())
	if !ok {
		// create new c8n
		_conn, err := c.getNew(action)
		if err != nil {
			log.Error("getNew fail: %s", err.Error())
			return nil, err
		}
		connListUntyped = []connectionStore{_conn}
	}

	connList, ok := connListUntyped.([]connectionStore)
	if !ok {
		log.Fatal("invalid connList")
		panic("unexpected conn_pool value")
	}
	if len(connList) <= 0 {
		log.Fatal("invalid connList")
		panic("unexpected conn_pool value")
	}
	var conn connectionStore
	i := 0
	for _, c := range connList {
		if c == nil {
			continue
		}
		// todo: check is conn is alive
		conn = c
		i++
		break
	}
	if conn == nil {
		panic("unexpected conn is nil")
	}
	connList = connList[1:]
	if len(connList) > 0 {
		c.idle.Store(action.GetKey(), connList)
	} else {
		c.idle.Delete(action.GetKey())
	}
	c.running.Store(conn.GetKey(), conn)

	return conn.(net.Conn), nil
}

func (c *connPool) Put(conn net.Conn) {
	_conn, ok := conn.(connectionStore)
	if !ok {
		return
	}
	if _conn.GetConnAction().ForceNew {
		err := conn.Close()
		if err != nil {
			log.Warn("conn.Close() fail: %s", err.Error())
		}
		return
	}
	k := _conn.GetKey()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	// del from running queue
	_, ok = c.running.Load(k)
	if ok {
		c.running.Delete(k)
	}

	// append to idle queue
	listUntyped, ok := c.idle.Load(_conn.GetIdleKey())
	if !ok {
		listUntyped = make([]connectionStore, 0, 1)
	}
	list, ok := listUntyped.([]connectionStore)
	if !ok {
		panic("unexpected type")
	}
	list = append(list, _conn)
	c.idle.Store(_conn.GetIdleKey(), list)

	return
}

func (c *connPool) getNew(action ConnAction) (*c8n, error) {
	addr := action.URL.Host
	if action.URL.Port() == "" {
		switch action.URL.Scheme {
		case "http":
			addr += ":80"
		case "https":
			addr += ":443"
		default:
			addr += ":80"
		}
	}
	log.Debug("getNew Dial addr: %s", addr)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	if action.URL.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true,
		})
		if err := tlsConn.Handshake(); err != nil {
			log.Error("shake hand fail: %s", err.Error())
			return nil, err
		}
		conn = tlsConn
	}
	_conn := &c8n{
		conn:    conn,
		r:       bufio.NewReader(conn),
		key:     action.GetKey() + ":" + strconv.FormatInt(time.Now().UnixNano(), 10),
		idleKey: action.GetKey(),
		action:  action,
	}
	return _conn, nil
}

type connectionStore interface {
	GetConnAction() ConnAction
	GetKey() string
	GetIdleKey() string
}

type c8n struct {
	conn    net.Conn
	r       *bufio.Reader
	key     string
	idleKey string
	action  ConnAction
}

func (c *c8n) Write(b []byte) (n int, err error) {
	return c.conn.Write(b)
}

func (c *c8n) Close() error {
	return c.Close()
}

func (c *c8n) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *c8n) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *c8n) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *c8n) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *c8n) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *c8n) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

func (c *c8n) Peek(n int) ([]byte, error) {
	return c.r.Peek(n)
}

func (c *c8n) GetKey() string {
	return c.key
}

func (c *c8n) GetIdleKey() string {
	return c.idleKey
}

func (c *c8n) GetConnAction() ConnAction {
	return c.action
}
