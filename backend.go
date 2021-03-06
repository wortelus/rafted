package rafted

import (
    cm "github.com/hhkbp2/rafted/comm"
    ev "github.com/hhkbp2/rafted/event"
    logging "github.com/hhkbp2/rafted/logging"
    ps "github.com/hhkbp2/rafted/persist"
    "io"
)

type Notifiable interface {
    GetNotifyChan() <-chan ev.NotifyEvent
}

type Backend interface {
    Send(event ev.RequestEvent)
    io.Closer
    Notifiable
}

type HSMBackend struct {
    local  Local
    peers  Peers
    server cm.Server
}

func (self *HSMBackend) Send(event ev.RequestEvent) {
    self.local.Send(event)
}

func (self *HSMBackend) GetNotifyChan() <-chan ev.NotifyEvent {
    return self.local.Notifier().GetNotifyChan()
}

func (self *HSMBackend) Close() error {
    toClose := make([]io.Closer, 0, 3)
    toClose = append(toClose, self.server, self.peers, self.local)
    ParallelClose(toClose)
    return nil
}

func NewHSMBackend(
    config *Configuration,
    localAddr *ps.ServerAddress,
    bindAddr *ps.ServerAddress,
    configManager ps.ConfigManager,
    stateMachine ps.StateMachine,
    log ps.Log,
    logger logging.Logger) (*HSMBackend, error) {

    local, err := NewLocalManager(
        config,
        localAddr,
        log,
        stateMachine,
        configManager,
        logger)
    if err != nil {
        return nil, err
    }
    client := cm.NewSocketClient(config.CommPoolSize, config.CommClientTimeout)
    getLoggerForPeer := func(_ ps.MultiAddr) logging.Logger {
        return logger
    }
    peerManager := NewPeerManager(
        config,
        client,
        local,
        getLoggerForPeer,
        logger)
    eventHandler := func(event ev.RequestEvent) {
        local.Send(event)
    }
    server, err := cm.NewSocketServer(
        cm.FirstAddr(bindAddr), config.CommServerTimeout, eventHandler, logger)
    if err != nil {
        // TODO add cleanup
        return nil, err
    }
    server.Serve()
    return &HSMBackend{
        local:  local,
        peers:  peerManager,
        server: server,
    }, nil
}
