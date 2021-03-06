package comm

import (
    "bufio"
    "errors"
    hsm "github.com/hhkbp2/go-hsm"
    ev "github.com/hhkbp2/rafted/event"
    logging "github.com/hhkbp2/rafted/logging"
    ps "github.com/hhkbp2/rafted/persist"
    "github.com/ugorji/go/codec"
    "io"
    "net"
    "sync"
    "time"
)

func ReadN(reader io.Reader, b []byte) (int, error) {
    length := len(b)
    i := 0
    for i < length {
        n, err := reader.Read(b[i:])
        if err == io.EOF {
            i += n
            return i, err
        } else if err != nil {
            return i, err
        }
        i += n
    }
    return i, nil
}

func WriteN(writer io.Writer, b []byte) (int, error) {
    length := len(b)
    i := 0
    for i < length {
        n, err := writer.Write(b[i:])
        if err != nil {
            return i, err
        }
        i += n
    }
    return i, nil
}

type SocketTransport struct {
    addr         net.Addr
    conn         net.Conn
    readTimeout  time.Duration
    writeTimeout time.Duration
}

func NewSocketTransport(addr net.Addr, timeout time.Duration) *SocketTransport {
    return &SocketTransport{
        addr:         addr,
        readTimeout:  timeout,
        writeTimeout: timeout,
    }
}

func (self *SocketTransport) Open() error {
    conn, err := net.DialTimeout(
        self.addr.Network(), self.addr.String(), self.readTimeout)
    if err != nil {
        return err
    }
    self.conn = conn
    return nil
}

func (self *SocketTransport) Close() error {
    return self.conn.Close()
}

func (self *SocketTransport) Read(b []byte) (int, error) {
    self.conn.SetReadDeadline(time.Now().Add(self.readTimeout))
    return self.conn.Read(b)
}

func (self *SocketTransport) Write(b []byte) (int, error) {
    self.conn.SetWriteDeadline(time.Now().Add(self.writeTimeout))
    return self.conn.Write(b)
}

func (self *SocketTransport) PeerAddr() net.Addr {
    return self.conn.RemoteAddr()
}

func (self *SocketTransport) SetReadTimeout(timeout time.Duration) {
    self.readTimeout = timeout
}

func (self *SocketTransport) SetWriteTimeout(timeout time.Duration) {
    self.writeTimeout = timeout
}

type SocketConnection struct {
    *SocketTransport
    reader  *bufio.Reader
    writer  *bufio.Writer
    encoder Encoder
    decoder Decoder
}

func NewSocketConnection(
    addr net.Addr, timeout time.Duration) *SocketConnection {

    conn := &SocketConnection{
        SocketTransport: NewSocketTransport(addr, timeout),
    }
    conn.reader = bufio.NewReader(conn.SocketTransport)
    conn.writer = bufio.NewWriter(conn.SocketTransport)
    conn.decoder = codec.NewDecoder(conn.reader, &codec.MsgpackHandle{})
    conn.encoder = codec.NewEncoder(conn.writer, &codec.MsgpackHandle{})
    return conn
}

func (self *SocketConnection) CallRPC(
    request ev.Event) (response ev.Event, err error) {

    if err := WriteEvent(self.writer, self.encoder, request); err != nil {
        self.Close()
        return nil, err
    }

    event, err := ReadResponse(self.reader, self.decoder)
    if err != nil {
        self.Close()
        return nil, err
    }

    return event, nil
}

type SocketClient struct {
    connectionPool     map[string][]*SocketConnection
    connectionPoolLock sync.Mutex

    poolSize int
    timeout  time.Duration
}

func NewSocketClient(poolSize int, timeout time.Duration) *SocketClient {
    return &SocketClient{
        connectionPool: make(map[string][]*SocketConnection),
        poolSize:       poolSize,
        timeout:        timeout,
    }
}

func (self *SocketClient) CallRPCTo(
    target1 ps.MultiAddr, request ev.Event) (response ev.Event, err error) {

    target, err := ps.FirstAddr(target1)
    if err != nil {
        return nil, err
    }
    connection, err := self.getConnection(target)
    if err != nil {
        return nil, err
    }

    response, err = connection.CallRPC(request)
    if err == nil {
        self.returnConnectionToPool(connection)
    }
    return response, err
}

func (self *SocketClient) getConnectionFromPool(
    target net.Addr) (*SocketConnection, error) {

    self.connectionPoolLock.Lock()
    defer self.connectionPoolLock.Unlock()

    key := target.String()
    connections, ok := self.connectionPool[key]
    if !ok || len(connections) == 0 {
        return nil, errors.New("no connection for this target")
    }

    connection := connections[len(connections)-1]
    self.connectionPool[key] = connections[:len(connections)-1]
    return connection, nil
}

func (self *SocketClient) returnConnectionToPool(
    connection *SocketConnection) {

    self.connectionPoolLock.Lock()
    defer self.connectionPoolLock.Unlock()

    key := connection.PeerAddr().String()
    connections, ok := self.connectionPool[key]
    if !ok {
        connections = make([]*SocketConnection, 0)
        self.connectionPool[key] = connections
    }

    if len(connections) < self.poolSize {
        self.connectionPool[key] = append(connections, connection)
    } else {
        connection.Close()
    }
}

func (self *SocketClient) getConnection(
    target net.Addr) (*SocketConnection, error) {

    // check for pooled connection first
    connection, err := self.getConnectionFromPool(target)
    if connection != nil && err == nil {
        return connection, nil
    }

    // if there is no pooled connection, create a new one
    connection = NewSocketConnection(target, self.timeout)
    if err := connection.Open(); err != nil {
        return nil, err
    }

    return connection, nil
}

func (self *SocketClient) Close() error {
    self.connectionPoolLock.Lock()
    defer self.connectionPoolLock.Unlock()

    var err error
    for target, connections := range self.connectionPool {
        for _, connection := range connections {
            err = connection.Close()
        }
        delete(self.connectionPool, target)
    }
    return err
}

type SocketServer struct {
    bindAddr     net.Addr
    readTimeout  time.Duration
    writeTimeout time.Duration
    listener     net.Listener
    group        sync.WaitGroup
    eventHandler RequestEventHandler
    logger       logging.Logger
}

func NewSocketServer(
    bindAddr net.Addr,
    timeout time.Duration,
    eventHandler RequestEventHandler,
    logger logging.Logger) (*SocketServer, error) {

    listener, err := net.Listen(bindAddr.Network(), bindAddr.String())
    if err != nil {
        return nil, err
    }
    object := &SocketServer{
        bindAddr:     bindAddr,
        readTimeout:  timeout,
        writeTimeout: timeout,
        listener:     listener,
        eventHandler: eventHandler,
        logger:       logger,
    }
    return object, nil
}

func (self *SocketServer) SetReadTimeout(timeout time.Duration) {
    self.readTimeout = timeout
}

func (self *SocketServer) SetWriteTimeout(timeout time.Duration) {
    self.writeTimeout = timeout
}

func (self *SocketServer) Serve() {
    routine := func() {
        self.group.Add(1)
        defer self.group.Done()
        for {
            conn, err := self.listener.Accept()
            if err != nil {
                self.logger.Debug(
                    "error: %s on accept, server about to exit", err)
                return
            }
            go self.handleConn(conn)
        }
    }
    go routine()
}

func (self *SocketServer) handleConn(conn net.Conn) {
    defer conn.Close()
    reader := bufio.NewReader(conn)
    writer := bufio.NewWriter(conn)
    decoder := codec.NewDecoder(reader, &codec.MsgpackHandle{})
    encoder := codec.NewEncoder(writer, &codec.MsgpackHandle{})

    for {
        conn.SetReadDeadline(time.Now().Add(self.readTimeout))
        if err := self.handleCommand(
            reader, writer, decoder, encoder); err != nil {

            if err != io.EOF {
                self.logger.Error(
                    "fail to handle command from connection: %s, error: %s",
                    conn.RemoteAddr().String(), err)
            }
            return
        }
        conn.SetWriteDeadline(time.Now().Add(self.writeTimeout))
        if err := writer.Flush(); err != nil {
            self.logger.Error("fail to write to connection: %s, error: %s",
                conn.RemoteAddr().String(), err)
            return
        }
    }
}

func (self *SocketServer) handleCommand(
    reader *bufio.Reader,
    writer *bufio.Writer,
    decoder Decoder,
    encoder Encoder) error {

    // read request
    event, err := ReadRequest(reader, decoder)
    if err != nil {
        return err
    }

    // dispatch event
    self.eventHandler(event)
    // wait for response
    response := event.RecvResponse()
    // send response
    if err := WriteEvent(writer, encoder, response); err != nil {
        return err
    }
    return nil
}

func (self *SocketServer) Close() error {
    err := self.listener.Close()
    self.group.Wait()
    return err
}

type SocketNetworkLayer struct {
    *SocketClient
    *SocketServer
}

func NewSocketNetworkLayer(
    client *SocketClient,
    server *SocketServer) *SocketNetworkLayer {
    return &SocketNetworkLayer{
        SocketClient: client,
        SocketServer: server,
    }
}

func WriteEvent(
    writer *bufio.Writer,
    encoder Encoder,
    event ev.Event) error {
    // write event type as first byte
    if err := writer.WriteByte(byte(event.Type())); err != nil {
        return err
    }
    // write the content of event
    if err := encoder.Encode(event.Message()); err != nil {
        return err
    }

    return writer.Flush()
}

func ReadRequest(
    reader *bufio.Reader,
    decoder Decoder) (ev.RequestEvent, error) {

    eventType, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    switch hsm.EventType(eventType) {
    case ev.EventAppendEntriesRequest:
        request := &ev.AppendEntriesRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewAppendEntriesRequestEvent(request)
        return event, nil
    case ev.EventRequestVoteRequest:
        request := &ev.RequestVoteRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewRequestVoteRequestEvent(request)
        return event, nil
    case ev.EventInstallSnapshotRequest:
        request := &ev.InstallSnapshotRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewInstallSnapshotRequestEvent(request)
        return event, nil
    case ev.EventClientAppendRequest:
        request := &ev.ClientAppendRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewClientAppendRequestEvent(request)
        return event, nil
    case ev.EventClientReadOnlyRequest:
        request := &ev.ClientReadOnlyRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewClientReadOnlyRequestEvent(request)
        return event, nil
    case ev.EventClientGetConfigRequest:
        request := &ev.ClientGetConfigRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewClientGetConfigRequestEvent(request)
        return event, nil
    case ev.EventClientChangeConfigRequest:
        request := &ev.ClientChangeConfigRequest{}
        if err := decoder.Decode(request); err != nil {
            return nil, err
        }
        event := ev.NewClientChangeConfigRequestEvent(request)
        return event, nil
    default:
        return nil, errors.New("not request event")
    }
}

func ReadResponse(
    reader *bufio.Reader,
    decoder Decoder) (ev.Event, error) {

    eventType, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    switch hsm.EventType(eventType) {
    case ev.EventAppendEntriesResponse:
        response := &ev.AppendEntriesResponse{}
        if err := decoder.Decode(response); err != nil {
            return nil, err
        }
        event := ev.NewAppendEntriesResponseEvent(response)
        return event, nil
    case ev.EventRequestVoteResponse:
        response := &ev.RequestVoteResponse{}
        if err := decoder.Decode(response); err != nil {
            return nil, err
        }
        event := ev.NewRequestVoteResponseEvent(response)
        return event, nil
    case ev.EventInstallSnapshotResponse:
        response := &ev.InstallSnapshotResponse{}
        if err := decoder.Decode(response); err != nil {
            return nil, err
        }
        event := ev.NewInstallSnapshotResponseEvent(response)
        return event, nil
    case ev.EventClientResponse:
        response := &ev.ClientResponse{}
        if err := decoder.Decode(response); err != nil {
            return nil, err
        }
        event := ev.NewClientResponseEvent(response)
        return event, nil
    case ev.EventClientGetConfigResponse:
        response := &ev.ClientGetConfigResponse{}
        if err := decoder.Decode(response); err != nil {
            return nil, err
        }
        event := ev.NewClientGetConfigResponseEvent(response)
        return event, nil
    case ev.EventLeaderRedirectResponse:
        response := &ev.LeaderRedirectResponse{}
        if err := decoder.Decode(response); err != nil {
            return nil, err
        }
        event := ev.NewLeaderRedirectResponseEvent(response)
        return event, nil
    case ev.EventLeaderUnknownResponse:
        event := ev.NewLeaderUnknownResponseEvent()
        return event, nil
    case ev.EventLeaderUnsyncResponse:
        event := ev.NewLeaderUnsyncResponseEvent()
        return event, nil
    case ev.EventLeaderInMemberChangeResponse:
        event := ev.NewLeaderInMemberChangeResponseEvent()
        return event, nil
    case ev.EventPersistErrorResponse:
        var err error
        if err := decoder.Decode(err); err != nil {
            return nil, err
        }
        event := ev.NewPersistErrorResponseEvent(err)
        return event, nil
    }
    return nil, errors.New("not request event")
}
