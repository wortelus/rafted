package rafted

import (
    "fmt"
    hsm "github.com/hhkbp2/go-hsm"
    ev "github.com/hhkbp2/rafted/event"
)

type LeaderState struct {
    *hsm.StateHead

    // TODO add fields
}

func NewLeaderState(super hsm.State) *LeaderState {
    object := &LeaderState{hsm.NewStateHead(super)}
    super.AddChild(object)
    return object
}

func (*LeaderState) ID() string {
    return StateLeaderID
}

func (self *LeaderState) Entry(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    fmt.Println(self.ID(), "-> Entry")
    raftHSM, ok := sm.(*RaftHSM)
    hsm.AssertTrue(ok)
    // init global status
    raftHSM.SetLeader(raftHSM.LocalAddr)
    // init status for this state

    return nil
}

func (self *LeaderState) Exit(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    fmt.Println(self.ID(), "-> Exit")
    return nil
}

func (self *LeaderState) Handle(sm hsm.HSM, event hsm.Event) (state hsm.State) {
    fmt.Println(self.ID(), "-> Handle, event=", event)
    switch {
    case event.Type() == ev.EventRequestVoteRequest:
        // TODO DEBUG
        fmt.Println("Leader possible step down")
        return nil
    case ev.IsClientEvent(event.Type()):
        fmt.Println("Leader process request")
        return nil
    }
    return self.Super()
}