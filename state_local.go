package rafted

import (
    hsm "github.com/hhkbp2/go-hsm"
    ev "github.com/hhkbp2/rafted/event"
    logging "github.com/hhkbp2/rafted/logging"
)

type LocalState struct {
    *LogStateHead
}

func NewLocalState(super hsm.State, logger logging.Logger) *LocalState {
    object := &LocalState{
        LogStateHead: NewLogStateHead(super, logger),
    }
    super.AddChild(object)
    return object
}

func (*LocalState) ID() string {
    return StateLocalID
}

func (self *LocalState) Entry(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    self.Debug("STATE: %s, -> Entry", self.ID())
    // ignore events
    return nil
}

func (self *LocalState) Init(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    self.Debug("STATE: %s, -> Init", self.ID())
    sm.QInit(StateFollowerID)
    return nil
}

func (self *LocalState) Exit(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    self.Debug("STATE: %s, -> Exit", self.ID())
    // ignore events
    return nil
}

func (self *LocalState) Handle(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    self.Debug("STATE: %s, -> Handle event: %s", self.ID(),
        ev.PrintEvent(event))
    // TODO add event handling if needed
    return nil
}

type NeedPeersState struct {
    *LogStateHead
}

func NewNeedPeersState(super hsm.State, logger logging.Logger) *NeedPeersState {
    object := &NeedPeersState{
        LogStateHead: NewLogStateHead(super, logger),
    }
    super.AddChild(object)
    return object
}

func (*NeedPeersState) ID() string {
    return StateNeedPeersID
}

func (self *NeedPeersState) Entry(
    sm hsm.HSM, event hsm.Event) (state hsm.State) {

    self.Debug("STATE: %s, -> Entry", self.ID())
    localHSM, ok := sm.(*LocalHSM)
    hsm.AssertTrue(ok)
    // coordinate peer into ActivatedPeerState
    localHSM.PeerManager().Broadcast(ev.NewPeerActivateEvent())
    return nil
}

func (self *NeedPeersState) Exit(
    sm hsm.HSM, event hsm.Event) (state hsm.State) {

    self.Debug("STATE: %s, -> Exit", self.ID())
    localHSM, ok := sm.(*LocalHSM)
    hsm.AssertTrue(ok)
    // coordinate peer into DeactivatePeerState
    localHSM.PeerManager().Broadcast(ev.NewPeerDeactivateEvent())
    return nil
}

func (self *NeedPeersState) Handle(
    sm hsm.HSM, event hsm.Event) (state hsm.State) {

    self.Debug("STATE: %s, -> Handle event: %s", self.ID(),
        ev.PrintEvent(event))
    // TODO add log
    return self.Super()
}