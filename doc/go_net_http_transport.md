# transport.go

*源码版本 go 1.12*

接口RoundTripper是用来

1. 完成一次http事务(Transaction)
2. 需要并发安全
3. 只用来实现底层的一次事务
    1. 不处理高级细节
    2. 除了消费request和关闭request，不对request进行任何其他操作
    3. 调用者不可在response关闭前，重用或修改request
4. 实现一定要关闭body

## Transport

有趣的事情：

Transport是一个对于chan很好的使用例子。

Transport持有一个LRU的闲置连接池，

1. 调用 ```getConn``` 方法继而调用 ```queueForIdleConn``` 和 ```queueForDial``` 增加获取链接的任务。
1. 通过gorountine进行拨号，成功之后查找是否有caller等待该链接，通过chan通知 *close(wantConn.Ready)*
1. 放回的时候也会检查

### roundTrip

默认RoundTripper的roundTrip方法的实现。

```golang
// roundTrip implements a RoundTripper over HTTP.
// RoundTrip的实现
func (t *Transport) roundTrip(req *Request) (*Response, error) {
	t.nextProtoOnce.Do(t.onceSetNextProtoDefaults) // 初始化ALPN（应用层协议协商）相关逻辑
	ctx := req.Context()
	trace := httptrace.ContextClientTrace(ctx) // 处理trace相关功能

	// 检查参数
	if req.URL == nil {
		req.closeBody()
		return nil, errors.New("http: nil Request.URL")
	}
	if req.Header == nil {
		req.closeBody()
		return nil, errors.New("http: nil Request.Header")
	}

	scheme := req.URL.Scheme
	isHTTP := scheme == "http" || scheme == "https"
	if isHTTP { // 如果是HTTP请求，检查Header的key和value是否合法
		for k, vv := range req.Header {
			if !httpguts.ValidHeaderFieldName(k) {
				req.closeBody()
				return nil, fmt.Errorf("net/http: invalid header field name %q", k)
			}
			for _, v := range vv {
				if !httpguts.ValidHeaderFieldValue(v) {
					req.closeBody()
					return nil, fmt.Errorf("net/http: invalid header field value %q for key %v", v, k)
				}
			}
		}
	}

	// 检查是否应用特殊的RoundTripper
	if altRT := t.alternateRoundTripper(req); altRT != nil {
		if resp, err := altRT.RoundTrip(req); err != ErrSkipAltProtocol {
			return resp, err
		}
	}
	// 检查必要的参数
	if !isHTTP {
		req.closeBody()
		return nil, &badStringError{"unsupported protocol scheme", scheme}
	}
	if req.Method != "" && !validMethod(req.Method) {
		req.closeBody()
		return nil, fmt.Errorf("net/http: invalid method %q", req.Method)
	}
	if req.URL.Host == "" {
		req.closeBody()
		return nil, errors.New("http: no Host in request URL")
	}

	for {
		select {
		case <-ctx.Done():
			req.closeBody()
			return nil, ctx.Err()
		default:
		}

		// treq gets modified by roundTrip, so we need to recreate for each retry.
		// 获得对request的包装
		treq := &transportRequest{Request: req, trace: trace}
		// 根据request获得连接方法(connectionMethod)
		cm, err := t.connectMethodForRequest(treq)
		if err != nil {
			req.closeBody()
			return nil, err
		}

		// Get the cached or newly-created connection to either the
		// host (for http or https), the http proxy, or the http proxy
		// pre-CONNECTed to https server. In any case, we'll be ready
		// to send it requests.
		// 根据request和连接方法获得连接
		pconn, err := t.getConn(treq, cm)
		if err != nil {
			t.setReqCanceler(req, nil)
			req.closeBody()
			return nil, err
		}

		var resp *Response
		if pconn.alt != nil {
			// 如果要升级协议
			// HTTP/2 path.
			t.setReqCanceler(req, nil) // not cancelable with CancelRequest
			resp, err = pconn.alt.RoundTrip(req)
		} else {
			// 走当前协议
			resp, err = pconn.roundTrip(treq)
		}
		if err == nil {
			// 没有发生异常，返回结果
			return resp, nil
		}

		// Failed. Clean up and determine whether to retry.
		// 失败，检查是否需要重试及清理现场

		_, isH2DialError := pconn.alt.(http2erringRoundTripper)
		if http2isNoCachedConnError(err) || isH2DialError {
			// 如果是HTTP2请求拨号错误或者 NoCachedConnError ，则清除该连接
			if t.removeIdleConn(pconn) {
				t.decConnsPerHost(pconn.cacheKey)
			}
		}
		// 检查是否需要重试
		if !pconn.shouldRetryRequest(req, err) {
			// Issue 16465: return underlying net.Conn.Read error from peek,
			// as we've historically done.
			if e, ok := err.(transportReadFromServerError); ok {
				err = e.err
			}
			return nil, err
		}
		testHookRoundTripRetried()

		// 重置request的body
		if req.GetBody != nil {
			newReq := *req
			var err error
			newReq.Body, err = req.GetBody()
			if err != nil {
				return nil, err
			}
			req = &newReq
		}
	}
}
```

### getConn

```golang
// getConn dials and creates a new persistConn to the target as
// specified in the connectMethod. This includes doing a proxy CONNECT
// and/or setting up TLS.  If this doesn't return an error, the persistConn
// is ready to write requests to.
// 返回一个persistConn
// 方法会完成 CONNECT 交互 和 TLS的初始化（如果需要的话）
// 当err为nil时，persistConn处于可以写入请求的状态
func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (pc *persistConn, err error) {
	req := treq.Request
	trace := treq.trace
	ctx := req.Context()
	if trace != nil && trace.GetConn != nil {
		trace.GetConn(cm.addr())
	}

	w := &wantConn{
		cm:         cm, // 链接的要求
		key:        cm.key(),
		ctx:        ctx,
		ready:      make(chan struct{}, 1),
		beforeDial: testHookPrePendingDial,
		afterDial:  testHookPostPendingDial,
	}
	defer func() {
		if err != nil {
			w.cancel(t, err)
		}
	}()

	// Queue for idle connection.
	if delivered := t.queueForIdleConn(w); delivered { // 尝试从idleLRU查询链接并返回给caller
		// 成功返回
		pc := w.pc
		// Trace only for HTTP/1.
		// HTTP/2 calls trace.GotConn itself.
		if pc.alt == nil && trace != nil && trace.GotConn != nil {
			trace.GotConn(pc.gotIdleConnTrace(pc.idleAt))
		}
		// set request canceler to some non-nil function so we
		// can detect whether it was cleared between now and when
		// we enter roundTrip
		t.setReqCanceler(req, func(error) {})
		return pc, nil
	}

	cancelc := make(chan error, 1)
	t.setReqCanceler(req, func(err error) { cancelc <- err })

	// Queue for permission to dial.
	t.queueForDial(w) // 等待拨号

	// Wait for completion or cancellation.
	// 等待拨号结果或取消
	select {
	case <-w.ready:
		// Trace success but only for HTTP/1.
		// HTTP/2 calls trace.GotConn itself.
		if w.pc != nil && w.pc.alt == nil && trace != nil && trace.GotConn != nil {
			trace.GotConn(httptrace.GotConnInfo{Conn: w.pc.conn, Reused: w.pc.isReused()})
		}
		if w.err != nil {
			// If the request has been cancelled, that's probably
			// what caused w.err; if so, prefer to return the
			// cancellation error (see golang.org/issue/16049).
			select {
			case <-req.Cancel:
				return nil, errRequestCanceledConn
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case err := <-cancelc:
				if err == errRequestCanceled {
					err = errRequestCanceledConn
				}
				return nil, err
			default:
				// return below
			}
		}
		return w.pc, w.err
	case <-req.Cancel:
		return nil, errRequestCanceledConn
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case err := <-cancelc:
		if err == errRequestCanceled {
			err = errRequestCanceledConn
		}
		return nil, err
	}
}
```

### queueForIdleConn

尝试获取闲置的链接

```golang
// queueForIdleConn queues w to receive the next idle connection for w.cm.
// As an optimization hint to the caller, queueForIdleConn reports whether
// it successfully delivered an already-idle connection.
func (t *Transport) queueForIdleConn(w *wantConn) (delivered bool) {
	if t.DisableKeepAlives {
		return false
	}

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	// Stop closing connections that become idle - we might want one.
	// (That is, undo the effect of t.CloseIdleConnections.)
	t.closeIdle = false

	if w == nil {
		// Happens in test hook.
		return false
	}

	// If IdleConnTimeout is set, calculate the oldest
	// persistConn.idleAt time we're willing to use a cached idle
	// conn.
	var oldTime time.Time
	if t.IdleConnTimeout > 0 {
		oldTime = time.Now().Add(-t.IdleConnTimeout)
	}

	// Look for most recently-used idle connection.
	if list, ok := t.idleConn[w.key]; ok { // 从lru中查找
		stop := false
		delivered := false
		for len(list) > 0 && !stop { // 遍历
			pconn := list[len(list)-1] // 查找最近使用过的链接

			// See whether this connection has been idle too long, considering
			// only the wall time (the Round(0)), in case this is a laptop or VM
			// coming out of suspend with previously cached idle connections.
			// 检查是否闲置太长时间
			tooOld := !oldTime.IsZero() && pconn.idleAt.Round(0).Before(oldTime)
			if tooOld {
				// Async cleanup. Launch in its own goroutine (as if a
				// time.AfterFunc called it); it acquires idleMu, which we're
				// holding, and does a synchronous net.Conn.Close.
				go pconn.closeConnIfStillIdle()
			}
			if pconn.isBroken() || tooOld {
				// 如果链接（被readLoop）标记为坏的或者闲置太长时间
				// 从清理掉
				// If either persistConn.readLoop has marked the connection
				// broken, but Transport.removeIdleConn has not yet removed it
				// from the idle list, or if this persistConn is too old (it was
				// idle too long), then ignore it and look for another. In both
				// cases it's already in the process of being closed.
				list = list[:len(list)-1]
				continue
			}
			delivered = w.tryDeliver(pconn, nil) // 尝试投递链接
			if delivered { // 投递成功
				if pconn.alt != nil {
					// http2可以多个client复用链接 所以保留在idle列表中
					// HTTP/2: multiple clients can share pconn.
					// Leave it in the list.
				} else {
					// 移除该链接
					// HTTP/1: only one client can use pconn.
					// Remove it from the list.
					t.idleLRU.remove(pconn)
					list = list[:len(list)-1]
				}
			}
			stop = true // 终止循环 已经尝试过投递了，结果会返回给caller
		}
		// 如果该key的链接已经没有闲置的了，清除该列表（防止leak）
		if len(list) > 0 {
			t.idleConn[w.key] = list
		} else {
			delete(t.idleConn, w.key)
		}
		if stop {
			// 如果找到了一个idle链接，且尝试过投递给链接的请求者，那么返回结果
			return delivered
		}
	}

	// Register to receive next connection that becomes idle.
	// 如果没有闲置链接
	// 记录对于该key的请求
	if t.idleConnWait == nil {
		t.idleConnWait = make(map[connectMethodKey]wantConnQueue)
	}
	q := t.idleConnWait[w.key]
	q.cleanFront()
	q.pushBack(w)
	t.idleConnWait[w.key] = q
	return false
}
```

### queueForDial

```golang
// queueForDial queues w to wait for permission to begin dialing.
// Once w receives permission to dial, it will do so in a separate goroutine.
// 排队拨号
func (t *Transport) queueForDial(w *wantConn) {
	w.beforeDial()
	if t.MaxConnsPerHost <= 0 { // 如果不限制每个host并发数，直接拨号
		go t.dialConnFor(w)
		return
	}

	t.connsPerHostMu.Lock()
	defer t.connsPerHostMu.Unlock()

	if n := t.connsPerHost[w.key]; n < t.MaxConnsPerHost {
		if t.connsPerHost == nil {
			t.connsPerHost = make(map[connectMethodKey]int)
		}
		t.connsPerHost[w.key] = n + 1
		go t.dialConnFor(w) // 如果没达到限制，拨号
		return
	}

	if t.connsPerHostWait == nil {
		t.connsPerHostWait = make(map[connectMethodKey]wantConnQueue)
	}
	q := t.connsPerHostWait[w.key]
	q.cleanFront() // 清理不在等待的wantConn
	q.pushBack(w) // 增加
	t.connsPerHostWait[w.key] = q
}
```

## persistConn

persistConn是对于net.Conn的包装，实现了```io.Reader```接口。

persistConn维持一个连接，通过一读一写两个任务channel，和readLoop/writeLoop两个循环来具体的实现了roundTrip的功能。

### 简单图示

![persistConn流程示意图](http://www.plantuml.com/plantuml/proxy?src=https://raw.githubusercontent.com/er1c-zh/digger/master/doc/go_http_transport_persist_conn.puml)

### 简单分析

分析下主要的几个方法:

- roundTrip
- readLoop
- writeLoop


```golang
func (pc *persistConn) roundTrip(req *transportRequest) (resp *Response, err error) {
	testHookEnterRoundTrip()
	if !pc.t.replaceReqCanceler(req.Request, pc.cancelRequest) {
		pc.t.putOrCloseIdleConn(pc)
		return nil, errRequestCanceled
	}
	pc.mu.Lock()
	pc.numExpectedResponses++
	headerFn := pc.mutateHeaderFunc
	pc.mu.Unlock()

	if headerFn != nil {
		headerFn(req.extraHeaders())
	}

	// 如果原生的请求没有要求Accept-Encoding且符合一些细小的case，那么这里会尝试使用gzip并解压
	requestedGzip := false
	if !pc.t.DisableCompression &&
		req.Header.Get("Accept-Encoding") == "" &&
		req.Header.Get("Range") == "" &&
		req.Method != "HEAD" {
		// Request gzip only, not deflate. Deflate is ambiguous and
		// not as universally supported anyway.
		// See: https://zlib.net/zlib_faq.html#faq39
		//
		// Note that we don't request this for HEAD requests,
		// due to a bug in nginx:
		//   https://trac.nginx.org/nginx/ticket/358
		//   https://golang.org/issue/5522
		//
		// We don't request gzip if the request is for a range, since
		// auto-decoding a portion of a gzipped document will just fail
		// anyway. See https://golang.org/issue/8923
		requestedGzip = true
		req.extraHeaders().Set("Accept-Encoding", "gzip")
	}

	// 判断 100-continue
	var continueCh chan struct{}
	if req.ProtoAtLeast(1, 1) && req.Body != nil && req.expectsContinue() {
		continueCh = make(chan struct{}, 1)
	}

	// 如果transport禁用了keep-alives并且client要求keep-alives，给Header加上"Connection:close"
	if pc.t.DisableKeepAlives && !req.wantsClose() {
		req.extraHeaders().Set("Connection", "close")
	}

	gone := make(chan struct{})
	defer close(gone)

	defer func() {
		if err != nil {
			pc.t.setReqCanceler(req.Request, nil)
		}
	}()

	const debugRoundTrip = false

	// Write the request concurrently with waiting for a response,
	// in case the server decides to reply before reading our full
	// request body.
	// 读写并行，为了解决服务器不读取全部请求便返回数据的情况
	startBytesWritten := pc.nwrite
	writeErrCh := make(chan error, 1)
	// 将请求写到 persistConn.writech 中，等待writeLoop消费
	pc.writech <- writeRequest{req, writeErrCh, continueCh}

	resc := make(chan responseAndError)
	// 将读取请求的“任务”写到persistConn.reqch中，等待readLoop消费
	pc.reqch <- requestAndChan{
		req:        req.Request,
		ch:         resc,
		addedGzip:  requestedGzip,
		continueCh: continueCh,
		callerGone: gone,
	}

	var respHeaderTimer <-chan time.Time
	cancelChan := req.Request.Cancel
	ctxDoneChan := req.Context().Done()
	// 循环处理 写异常、链接关闭、读、取消、Done 事件
	for {
		testHookWaitResLoop()
		select {
		case err := <-writeErrCh: // 写请求的时候发生异常，或者写入完成？
			if debugRoundTrip {
				req.logf("writeErrCh resv: %T/%#v", err, err)
			}
			if err != nil {
				pc.close(fmt.Errorf("write error: %v", err)) // 写异常了，关闭链接
				return nil, pc.mapRoundTripError(req, startBytesWritten, err) // 返回结果
			}
			// 没有异常，且有“写入请求完成，等待返回结果超时时间”，开始计时
			if d := pc.t.ResponseHeaderTimeout; d > 0 {
				if debugRoundTrip {
					req.logf("starting timer for %v", d)
				}
				timer := time.NewTimer(d)
				defer timer.Stop() // prevent leaks
				respHeaderTimer = timer.C
			}
		case <-pc.closech: // 链接关闭了
			if debugRoundTrip {
				req.logf("closech recv: %T %#v", pc.closed, pc.closed)
			}
			return nil, pc.mapRoundTripError(req, startBytesWritten, pc.closed)
		case <-respHeaderTimer: // 返回结果超时了
			if debugRoundTrip {
				req.logf("timeout waiting for response headers.")
			}
			pc.close(errTimeout)
			return nil, errTimeout
		case re := <-resc: // 读取到了结果，或发生了异常
			if (re.res == nil) == (re.err == nil) {
				panic(fmt.Sprintf("internal error: exactly one of res or err should be set; nil=%v", re.res == nil))
			}
			if debugRoundTrip {
				req.logf("resc recv: %p, %T/%#v", re.res, re.err, re.err)
			}
			if re.err != nil {
				// 读取结果发生异常
				return nil, pc.mapRoundTripError(req, startBytesWritten, re.err)
			}
			return re.res, nil // 正常读取到了结果，返回
		case <-cancelChan:
			pc.t.CancelRequest(req.Request) // 请求被主动取消了
			cancelChan = nil
		case <-ctxDoneChan: // 请求的context被完成了
			pc.t.cancelRequest(req.Request, req.Context().Err())
			cancelChan = nil
			ctxDoneChan = nil
		}
	}
}
```

持久连接对象的writeLoop方法，会消费pc.writech中的数据，向目标链接发送数据

```golang
func (pc *persistConn) writeLoop() {
	defer close(pc.writeLoopDone)
	for { // 循环处理请求
		select {
		case wr := <-pc.writech:
			startBytesWritten := pc.nwrite
			err := wr.req.Request.write(pc.bw, pc.isProxy, wr.req.extra, pc.waitForContinue(wr.continueCh)) // 调用 http.Request.write方法，向pc.bw(bufWriter)写入请求
			if bre, ok := err.(requestBodyReadError); ok {
				// 读取请求的body错误
				err = bre.error
				// Errors reading from the user's
				// Request.Body are high priority.
				// Set it here before sending on the
				// channels below or calling
				// pc.close() which tears town
				// connections and causes other
				// errors.
				wr.req.setError(err)
			}
			if err == nil {
				// 如果没有发生异常，刷新发送缓冲区
				err = pc.bw.Flush()
			}
			if err != nil {
				wr.req.Request.closeBody()
				if pc.nwrite == startBytesWritten {
					// pc.nwrite 在被包装的pc.bw中被修改
					// 如果没有变动，意味着没有数据被写入连接就发生异常
					err = nothingWrittenError{err}
				}
			}
			pc.writeErrCh <- err // to the body reader, which might recycle us
			wr.ch <- err         // to the roundTrip function
			if err != nil {
				pc.close(err)
				return
			}
			// 如果正常写入，循环等待下次任务
		case <-pc.closech: // 持久连接被关闭
			return
		}
	}
}
```

持久连接对象的readLoop方法，读取链接中的数据

```golang
func (pc *persistConn) readLoop() {
	closeErr := errReadLoopExiting // default value, if not changed below
	defer func() {
		pc.close(closeErr) // 关闭底层链接（如果没有被接管）和pc.closech
		pc.t.removeIdleConn(pc) // 从链接池移除链接
	}()

	// 用来放回链接，如果发生异常，返回false
	tryPutIdleConn := func(trace *httptrace.ClientTrace) bool {
		if err := pc.t.tryPutIdleConn(pc)/*tranport的放回方法*/; err != nil {
			closeErr = err
			if trace != nil && trace.PutIdleConn != nil && err != errKeepAlivesDisabled {
				trace.PutIdleConn(err)
			}
			return false
		}
		if trace != nil && trace.PutIdleConn != nil {
			trace.PutIdleConn(nil)
		}
		return true
	}

	// eofc is used to block caller goroutines reading from Response.Body
	// at EOF until this goroutines has (potentially) added the connection
	// back to the idle pool.
	eofc := make(chan struct{})
	defer close(eofc) // unblock reader on errors

	// Read this once, before loop starts. (to avoid races in tests)
	testHookMu.Lock()
	testHookReadLoopBeforeNextRead := testHookReadLoopBeforeNextRead
	testHookMu.Unlock()

	alive := true
	for alive { // 需要保持连接活跃
		pc.readLimit = pc.maxHeaderResponseSize()
		_, err := pc.br.Peek(1) // 检查链接

		pc.mu.Lock()
		if pc.numExpectedResponses == 0 {
			pc.readLoopPeekFailLocked(err)
			pc.mu.Unlock()
			return
		}
		pc.mu.Unlock()

		rc := <-pc.reqch // 获取读取任务
		trace := httptrace.ContextClientTrace(rc.req.Context())

		var resp *Response
		if err == nil {
			resp, err = pc.readResponse(rc, trace) // 尝试读取
		} else {
			err = transportReadFromServerError{err} // 链接peek出问题
			closeErr = err
		}

		if err != nil {
			if pc.readLimit <= 0 {
				err = fmt.Errorf("net/http: server response headers exceeded %d bytes; aborted", pc.maxHeaderResponseSize())
			}

			// 链接发生问题或者readResponse发生问题
			// 发送给该读取请求的caller
			select {
			case rc.ch <- responseAndError{err: err}:
			case <-rc.callerGone: // gone
				return
			}
			return
			// 出现异常 返回
		}
		pc.readLimit = maxInt64 // effectively no limit for response bodies

		pc.mu.Lock()
		pc.numExpectedResponses--
		pc.mu.Unlock()

		bodyWritable := resp.bodyIsWritable() // 用于判断链接是否是被其他流程接管
		hasBody := rc.req.Method != "HEAD" && resp.ContentLength != 0

		if resp.Close || rc.req.Close || resp.StatusCode <= 199 || bodyWritable {
			// Don't do keep-alive on error if either party requested a close
			// or we get an unexpected informational (1xx) response.
			// StatusCode 100 is already handled above.
			// 如果 resp关闭、请求关闭、过多的1xx、请求被接管，终止
			alive = false
		}

		if !hasBody || bodyWritable {
			pc.t.setReqCanceler(rc.req, nil)

			// Put the idle conn back into the pool before we send the response
			// so if they process it quickly and make another request, they'll
			// get this same conn. But we use the unbuffered channel 'rc'
			// to guarantee that persistConn.roundTrip got out of its select
			// potentially waiting for this persistConn to close.
			// but after
			// 1. 先放回链接，后返回结果，可以保证下一次请求的时候能复用
			// 2. 为了保证该持久链接对象的roundTrip方法从等待该持久链接对象
			// 关闭的select等待中退出，将readLoop阻塞在无缓冲的rc.ch上
			alive = alive &&
				!pc.sawEOF && // 没有读取到EOF
				pc.wroteRequest() && // 链接的写是否无错误的完成
				tryPutIdleConn(trace) // 尝试放回链接

			if bodyWritable {
				closeErr = errCallerOwnsConn
			}

			select {
			case rc.ch <- responseAndError{res: resp}: // 放回结果
			case <-rc.callerGone: // 请求取消
				return
			}

			// Now that they've read from the unbuffered channel, they're safely
			// out of the select that also waits on this goroutine to die, so
			// we're allowed to exit now if needed (if alive is false)
			// （接2）直到此时，就可以认为roundTrip方法从上述的等待中退出了
			// 那么如果需要的话(alive == false)，我们可以安全的从readLoop中退出（意味着关闭该链接）
			// （如果不这么做的话，可能会有结果已经返回，但是persistConn.roundTrip方法
			// 没有消费到的情况，因为在等待结果的select上，也等待persistConn的close事件）
			testHookReadLoopBeforeNextRead()
			continue
		}

		waitForBodyRead := make(chan bool, 2)
		// 一个对net.conn的io.ReadCloser包装，用于确保下一次读取之前，上一次请求的处理已经完成
		body := &bodyEOFSignal{ 
			body: resp.Body,
			earlyCloseFn: func() error {
				waitForBodyRead <- false
				<-eofc // will be closed by deferred call at the end of the function
				return nil

			},
			fn: func(err error) error {
				isEOF := err == io.EOF
				waitForBodyRead <- isEOF
				if isEOF {
					<-eofc // see comment above eofc declaration
				} else if err != nil {
					if cerr := pc.canceled(); cerr != nil {
						return cerr
					}
				}
				return err
			},
		}

		resp.Body = body
		if rc.addedGzip && strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
			// 处理transport添加的gzip
			resp.Body = &gzipReader{body: body}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
			resp.Uncompressed = true
		}

		select { // 等待读取或者请求取消
		case rc.ch <- responseAndError{res: resp}:
		case <-rc.callerGone:
			return
		}

		// Before looping back to the top of this function and peeking on
		// the bufio.Reader, wait for the caller goroutine to finish
		// reading the response body. (or for cancellation or death)
		select {
		case bodyEOF := <-waitForBodyRead: // bodyEOFSignal会在resp被成功读取到eof时写入true
			pc.t.setReqCanceler(rc.req, nil) // before pc might return to idle pool
			alive = alive && // 之前也是保持链接
				bodyEOF && // 读取到eof为true
				!pc.sawEOF && // 看到了eof
				pc.wroteRequest() && // 写请求完成
				tryPutIdleConn(trace) // 放回链接完成
			if bodyEOF {
				eofc <- struct{}{} // 同步 caller读取body到eof 和放回链接 的操作
			}
		case <-rc.req.Cancel: // 请求取消了
			alive = false
			pc.t.CancelRequest(rc.req)
		case <-rc.req.Context().Done(): // 请求的ctx已经完成
			alive = false
			pc.t.cancelRequest(rc.req, rc.req.Context().Err())
		case <-pc.closech: // 链接关闭了
			alive = false
		}

		testHookReadLoopBeforeNextRead()
	}
}
```