package rafted

import (
    hsm "github.com/hhkbp2/go-hsm"
    ev "github.com/hhkbp2/rafted/event"
    logging "github.com/hhkbp2/rafted/logging"
    ps "github.com/hhkbp2/rafted/persist"
    "github.com/hhkbp2/rafted/str"
    "github.com/hhkbp2/testify/assert"
    "github.com/hhkbp2/testify/mock"
    "testing"
    "time"
)

func testConfiguration() *Configuration {
    config := DefaultConfiguration()
    // increase the timeout to reduce the possibility of election split
    // in testing
    // config.HeartbeatTimeout = time.Millisecond * 200
    // config.ElectionTimeout = time.Second * 1
    // config.MaxTimeoutJitter = 0.2
    // config.ClientTimeout = config.HeartbeatTimeout
    return config
}

var (
    testConfig         = testConfiguration()
    testData           = []byte(str.RandomString(100))
    testIndex   uint64 = 100
    testTerm    uint64 = 10
    testServers        = ps.SetupMemoryMultiAddrSlice(3)
)

func getTestLog(
    committedIndex, lastAppliedIndex uint64,
    entries []*ps.LogEntry) (ps.Log, error) {

    log := ps.NewMemoryLog()
    if err := log.StoreLogs(entries); err != nil {
        return nil, err
    }
    if err := log.StoreCommittedIndex(committedIndex); err != nil {
        return nil, err
    }
    if err := log.StoreLastAppliedIndex(lastAppliedIndex); err != nil {
        return nil, err
    }
    return log, nil
}

func getTestLocal() (Local, error) {
    servers := testServers
    localAddr := servers.Addresses[0]
    index := testIndex
    term := testTerm
    conf := &ps.Config{
        Servers:    servers,
        NewServers: nil,
    }
    entries := []*ps.LogEntry{
        &ps.LogEntry{
            Term:  term,
            Index: index,
            Type:  ps.LogCommand,
            Data:  testData,
            Conf:  conf,
        },
    }
    log, err := getTestLog(index, index, entries)
    if err != nil {
        return nil, err
    }
    stateMachine := ps.NewMemoryStateMachine()
    configManager := ps.NewMemoryConfigManager(index, conf)
    logger := logging.GetLogger("test local")
    local, err := NewLocalManager(
        testConfig,
        localAddr,
        log,
        stateMachine,
        configManager,
        logger)
    if err != nil {
        return nil, err
    }
    return local, nil
}

func BeforeTimeout(timeout time.Duration, startTime time.Time) time.Duration {
    d := time.Duration(
        int64(float32(int64(timeout)) * (1 - testConfig.MaxTimeoutJitter)))
    return (d - time.Now().Sub(startTime))
}

type MockPeers struct {
    mock.Mock
}

func NewMockPeers(local Local) *MockPeers {
    object := &MockPeers{}
    local.SetPeers(object)
    return object
}

func (self *MockPeers) Broadcast(event hsm.Event) {
    self.Mock.Called(event)
}

func (self *MockPeers) AddPeers(peerAddrSlice *ps.ServerAddressSlice) {
    self.Mock.Called(peerAddrSlice)
}

func (self *MockPeers) RemovePeers(peerAddrSlice *ps.ServerAddressSlice) {
    self.Mock.Called(peerAddrSlice)
}

func (self *MockPeers) Close() error {
    args := self.Mock.Called()
    return args.Error(0)
}

func getTestLocalSafe(t *testing.T) Local {
    local, err := getTestLocal()
    assert.Nil(t, err)
    return local
}

func getTestLocalAndPeers(t *testing.T) (Local, *MockPeers) {
    local := getTestLocalSafe(t)
    return local, NewMockPeers(local)
}

// ------------------------------------------------------------
// raft events related
// ------------------------------------------------------------

func assertGetAppendEntriesRequestEvent(
    t *testing.T, eventChan <-chan hsm.Event, afterTime time.Duration,
    request *ev.AppendEntriesRequest) {

    select {
    case event := <-eventChan:
        assert.Equal(t, ev.EventAppendEntriesRequest, event.Type(),
            "expect %s but actual %s",
            ev.EventTypeString(ev.EventAppendEntriesRequest),
            ev.EventTypeString(event.Type()))
        e, ok := event.(*ev.AppendEntriesRequestEvent)
        assert.True(t, ok)
        assert.Equal(t, request.Term, e.Request.Term)
        // TODO add more fields check
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertGetRequestVoteResponseEvent(
    t *testing.T, reqEvent ev.RequestEvent, granted bool, term uint64) {

    respEvent := reqEvent.RecvResponse()
    assert.Equal(t, ev.EventRequestVoteResponse, respEvent.Type())
    event, ok := respEvent.(*ev.RequestVoteResponseEvent)
    assert.True(t, ok)
    assert.Equal(t, granted, event.Response.Granted)
    assert.Equal(t, term, event.Response.Term)
}

func assertGetAppendEntriesResponseEvent(t *testing.T,
    reqEvent ev.RequestEvent, success bool, term, index uint64) {

    respEvent := reqEvent.RecvResponse()
    assert.Equal(t, ev.EventAppendEntriesResponse, respEvent.Type())
    event, ok := respEvent.(*ev.AppendEntriesResponseEvent)
    assert.True(t, ok)
    assert.Equal(t, success, event.Response.Success)
    assert.Equal(t, term, event.Response.Term)
    assert.Equal(t, index, event.Response.LastLogIndex)
}

// ------------------------------------------------------------
// client events related
// ------------------------------------------------------------

func assertGetLeaderUnknownResponse(t *testing.T, reqEvent ev.RequestEvent) {
    respEvent := reqEvent.RecvResponse()
    assert.Equal(t, ev.EventLeaderUnknownResponse, respEvent.Type(),
        "expect %s but actual %s",
        ev.EventTypeString(ev.EventLeaderUnknownResponse),
        ev.EventTypeString(respEvent.Type()))
    _, ok := respEvent.(*ev.LeaderUnknownResponseEvent)
    assert.True(t, ok)
}

func assertGetLeaderUnsyncResponseEvent(t *testing.T, reqEvent ev.RequestEvent) {
    respEvent := reqEvent.RecvResponse()
    assert.Equal(t, ev.EventLeaderUnsyncResponse, respEvent.Type(),
        "expect %s but actual %s",
        ev.EventTypeString(ev.EventLeaderUnsyncResponse),
        ev.EventTypeString(respEvent.Type()))
    _, ok := respEvent.(*ev.LeaderUnsyncResponseEvent)
    assert.True(t, ok)
}

func assertGetClientResponseEvent(
    t *testing.T, reqEvent ev.RequestEvent, success bool, data []byte) {

    respEvent := reqEvent.RecvResponse()
    assert.Equal(t, ev.EventClientResponse, respEvent.Type(),
        "expect %s but actual %s",
        ev.EventTypeString(ev.EventClientResponse),
        ev.EventTypeString(respEvent.Type()))
    event, ok := respEvent.(*ev.ClientResponseEvent)
    assert.True(t, ok)
    assert.Equal(t, success, event.Response.Success)
    assert.Equal(t, data, event.Response.Data)
}

// ------------------------------------------------------------
// notify related
// ------------------------------------------------------------
func SwallowNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration,
    number int) {

    for i := 0; i < number; i++ {
        select {
        case event := <-notifyChan:
            assert.True(t, ev.IsNotifyEvent(event.Type()))
        case <-time.After(afterTime):
            assert.True(t, false)
        }
    }
}

func SwallowNotifyNow(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, number int) {

    SwallowNotify(t, notifyChan, 0, number)
}

func assertGetElectionTimeoutNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration) {

    select {
    case e := <-notifyChan:
        assert.Equal(t, ev.EventNotifyElectionTimeout, e.Type(),
            "expect %s, but actual %s",
            ev.NotifyTypeString(ev.EventNotifyElectionTimeout),
            ev.NotifyTypeString(e.Type()))
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertNotGetElectionTimeoutNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration) {

    select {
    case <-notifyChan:
        assert.True(t, false)
    case <-time.After(afterTime):
        // Do nothing
    }
}

func assertGetHeartbeatTimeoutNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration) {

    select {
    case e := <-notifyChan:
        assert.Equal(t, ev.EventNotifyHeartbeatTimeout, e.Type(),
            "expect %s, but actual %s",
            ev.NotifyTypeString(ev.EventNotifyHeartbeatTimeout),
            ev.NotifyTypeString(e.Type()))
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertGetStateChangeNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration,
    oldState, newState ev.RaftStateType) {

    select {
    case event := <-notifyChan:
        assert.Equal(t, ev.EventNotifyStateChange, event.Type(),
            "expect %s, but actual %s",
            ev.NotifyTypeString(ev.EventNotifyStateChange),
            ev.NotifyTypeString(event.Type()))
        e, ok := event.(*ev.NotifyStateChangeEvent)
        assert.True(t, ok)
        assert.Equal(t, oldState, e.OldState)
        assert.Equal(t, newState, e.NewState)
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertGetLeaderChangeNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration,
    leader *ps.ServerAddress) {

    select {
    case event := <-notifyChan:
        assert.Equal(t, ev.EventNotifyLeaderChange, event.Type(),
            "expect %s, but actual %s",
            ev.NotifyTypeString(ev.EventNotifyLeaderChange),
            ev.NotifyTypeString(event.Type()))
        e, ok := event.(*ev.NotifyLeaderChangeEvent)
        assert.True(t, ok)
        assert.True(t, ps.MultiAddrEqual(leader, e.NewLeader))
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertGetTermChangeNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration,
    oldTerm, newTerm uint64) {

    select {
    case event := <-notifyChan:
        assert.Equal(t, ev.EventNotifyTermChange, event.Type(),
            "expect %s but actual %s",
            ev.NotifyTypeString(ev.EventNotifyTermChange),
            ev.NotifyTypeString(event.Type()))
        e, ok := event.(*ev.NotifyTermChangeEvent)
        assert.True(t, ok)
        assert.Equal(t, oldTerm, e.OldTerm)
        assert.Equal(t, newTerm, e.NewTerm)
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertGetCommitNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration,
    term, index uint64) {

    select {
    case event := <-notifyChan:
        assert.Equal(t, ev.EventNotifyCommit, event.Type(),
            "expect %s but actual %s",
            ev.NotifyTypeString(ev.EventNotifyCommit),
            ev.NotifyTypeString(event.Type()))
        e, ok := event.(*ev.NotifyCommitEvent)
        assert.True(t, ok)
        assert.Equal(t, term, e.Term)
        assert.Equal(t, index, e.LogIndex)
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

func assertGetApplyNotify(
    t *testing.T, notifyChan <-chan ev.NotifyEvent, afterTime time.Duration,
    term, index uint64) {

    select {
    case event := <-notifyChan:
        assert.Equal(t, ev.EventNotifyApply, event.Type(),
            "expect %s but actual %s",
            ev.NotifyTypeString(ev.EventNotifyApply),
            ev.NotifyTypeString(event.Type()))
        e, ok := event.(*ev.NotifyApplyEvent)
        assert.True(t, ok)
        assert.Equal(t, term, e.Term)
        assert.Equal(t, index, e.LogIndex)
    case <-time.After(afterTime):
        assert.True(t, false)
    }
}

// ------------------------------------------------------------
// log related
// ------------------------------------------------------------

func assertLogLastIndex(t *testing.T, log ps.Log, index uint64) {
    lastLogIndex, err := log.LastIndex()
    assert.Nil(t, err)
    assert.Equal(t, index, lastLogIndex)
}

func assertLogLastTerm(t *testing.T, log ps.Log, term uint64) {
    lastTerm, err := log.LastTerm()
    assert.Nil(t, err)
    assert.Equal(t, term, lastTerm)
}

func assertLogCommittedIndex(t *testing.T, log ps.Log, index uint64) {
    committedIndex, err := log.CommittedIndex()
    assert.Nil(t, err)
    assert.Equal(t, index, committedIndex)
}

// ------------------------------------------------------------
// Test Case
// ------------------------------------------------------------
