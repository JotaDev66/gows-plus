package callctl

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/devlikeapro/gows/voip/call"
	"github.com/devlikeapro/gows/voip/core"
	"github.com/devlikeapro/gows/voip/signaling"
	"github.com/devlikeapro/gows/voip/wa"
	"github.com/devlikeapro/gows/voip/wanode"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const defaultMaxCalls = 8

type EmitFunc func(event interface{})

type CallStateEvent struct {
	CallID    string `json:"callId"`
	Peer      string `json:"peer"`
	Direction string `json:"direction"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Video     bool   `json:"video"`
}

type activeCall struct {
	cm       *call.CallManager
	audioIn  string
	audioOut string

	started bool
	source  *FileAudioSource
	sink    *FileAudioSink
	bridge  *Bridge
}

type Controller struct {
	cli      *whatsmeow.Client
	log      *slog.Logger
	emit     EmitFunc
	maxCalls int

	mu    sync.Mutex
	calls map[string]*activeCall
}

func NewController(cli *whatsmeow.Client, log *slog.Logger, emit EmitFunc) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{
		cli:      cli,
		log:      log,
		emit:     emit,
		maxCalls: defaultMaxCalls,
		calls:    make(map[string]*activeCall),
	}
}

func (c *Controller) StartCall(ctx context.Context, peer types.JID, audioIn, audioOut string) (string, error) {
	c.mu.Lock()
	n := len(c.calls)
	c.mu.Unlock()
	if c.maxCalls > 0 && n >= c.maxCalls {
		return "", fmt.Errorf("max concurrent calls reached (%d)", c.maxCalls)
	}

	callID := signaling.GenerateCallID()
	ac := c.createCall(callID, audioIn, audioOut)
	if err := ac.cm.StartCall(ctx, callID, peer, false); err != nil {
		c.cleanup(callID)
		return "", err
	}
	return callID, nil
}

func (c *Controller) AcceptCall(ctx context.Context, callID, audioIn, audioOut string) error {
	ac, ok := c.get(callID)
	if !ok {
		return fmt.Errorf("no call with id %s", callID)
	}
	c.mu.Lock()
	ac.audioIn = audioIn
	ac.audioOut = audioOut
	c.mu.Unlock()
	return ac.cm.AcceptCall(ctx, callID)
}

func (c *Controller) EndCall(ctx context.Context, callID string) error {
	ac, ok := c.get(callID)
	if !ok {
		return fmt.Errorf("no call with id %s", callID)
	}
	return ac.cm.EndCall(ctx, core.EndCallReasonUserEnded)
}

// WebRTC answers a browser SDP offer for the given call, opening the PCM data
// channel bridge (browser mic -> call uplink, peer audio -> browser). Returns
// the SDP answer.
func (c *Controller) WebRTC(callID, sdpOffer string) (string, error) {
	ac, ok := c.get(callID)
	if !ok {
		return "", fmt.Errorf("no call with id %s", callID)
	}
	bridge, answer, err := NewBridge(sdpOffer, c.log)
	if err != nil {
		return "", err
	}
	bridge.OnBrowserPCM = func(pcm []float32) {
		ac.cm.FeedCapturedPCM(pcm)
	}
	bridge.OnTerminalICE = func() {
		go c.EndCall(context.Background(), callID)
	}
	c.mu.Lock()
	if ac.bridge != nil {
		ac.bridge.Close()
	}
	ac.bridge = bridge
	c.mu.Unlock()
	return answer, nil
}

func (c *Controller) HandleEvent(evt any) {
	ctx := context.Background()
	switch e := evt.(type) {
	case *events.CallOffer:
		c.onOffer(ctx, e)
	case *events.CallAccept:
		if ac, ok := c.callFor(e.From, e.Data); ok {
			ac.cm.HandleCallAccept(ctx, wrapCall(e.From, e.Data), e.From)
		}
	case *events.CallTransport:
		if ac, ok := c.callFor(e.From, e.Data); ok {
			ac.cm.HandleCallTransport(ctx, wrapCall(e.From, e.Data), e.From)
		}
	case *events.CallTerminate:
		if ac, ok := c.callFor(e.From, e.Data); ok {
			ac.cm.HandleCallTerminate(wrapCall(e.From, e.Data))
		}
	case *events.CallReject:
		if ac, ok := c.callFor(e.From, e.Data); ok {
			ac.cm.HandleCallTerminate(wrapCall(e.From, e.Data))
		}
	}
}

func (c *Controller) onOffer(ctx context.Context, e *events.CallOffer) {
	node := wrapCall(e.From, e.Data)
	callID := callIDFromNode(node)
	if callID == "" {
		return
	}
	c.mu.Lock()
	n := len(c.calls)
	c.mu.Unlock()
	if c.maxCalls > 0 && n >= c.maxCalls {
		c.rejectOffer(ctx, node, e.From)
		return
	}
	ac := c.createCall(callID, "", "")
	ac.cm.HandleCallOffer(ctx, node, e.From)
}

func (c *Controller) rejectOffer(ctx context.Context, node *waBinary.Node, from types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
	if creator == "" {
		creator = from.String()
	}
	reject := signaling.BuildRejectStanza(from, info.CallID, wanode.MustJID(creator))
	if err := wa.NewSocket(c.cli).SendNode(ctx, reject); err != nil {
		c.log.Error("reject offer at capacity failed", "err", err)
	}
}

func (c *Controller) createCall(callID, audioIn, audioOut string) *activeCall {
	cm := call.NewCallManager(wa.NewSocket(c.cli), c.log)
	ac := &activeCall{cm: cm, audioIn: audioIn, audioOut: audioOut}
	c.wire(ac, callID)
	c.mu.Lock()
	c.calls[callID] = ac
	c.mu.Unlock()
	return ac
}

func (c *Controller) wire(ac *activeCall, callID string) {
	cm := ac.cm
	cm.OnIncoming = func(ci *call.CallInfo) {
		c.emitState(ci)
	}
	cm.OnStateChange = func(ci *call.CallInfo) {
		c.emitState(ci)
		if ci.IsEnded() {
			go c.cleanup(callID)
			return
		}
		if ci.StateData.State == core.CallStateActive {
			go c.onActive(callID)
		}
	}
	cm.OnEnded = func(ci *call.CallInfo) {
		c.emitState(ci)
		go c.cleanup(callID)
	}
	cm.OnPeerAudio = func(pcm []float32) {
		c.mu.Lock()
		bridge := ac.bridge
		sink := ac.sink
		c.mu.Unlock()
		if bridge != nil {
			_ = bridge.WritePCM(pcm)
			return
		}
		if sink != nil {
			_ = sink.WritePCM(pcm)
		}
	}
}

// onActive starts the audio bridge once, when the call reaches ACTIVE.
func (c *Controller) onActive(callID string) {
	c.mu.Lock()
	ac, ok := c.calls[callID]
	if !ok || ac.started {
		c.mu.Unlock()
		return
	}
	ac.started = true
	audioIn, audioOut := ac.audioIn, ac.audioOut
	c.mu.Unlock()

	if audioOut != "" {
		sink, err := NewFileAudioSink(audioOut)
		if err != nil {
			c.log.Error("open audio sink failed", "err", err, "path", audioOut)
		} else {
			c.mu.Lock()
			ac.sink = sink
			c.mu.Unlock()
		}
	}

	if audioIn != "" {
		frameSize := ac.cm.FrameSize()
		if frameSize <= 0 {
			c.log.Warn("codec unavailable; uplink audio disabled", "call_id", callID)
			return
		}
		src := NewFileAudioSource(audioIn)
		c.mu.Lock()
		ac.source = src
		c.mu.Unlock()
		if err := src.Start(ac.cm, frameSize); err != nil {
			c.log.Error("start audio source failed", "err", err, "path", audioIn)
		}
	}
}

func (c *Controller) cleanup(callID string) {
	c.mu.Lock()
	ac, ok := c.calls[callID]
	if ok {
		delete(c.calls, callID)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	if ac.source != nil {
		ac.source.Stop()
	}
	if ac.sink != nil {
		ac.sink.Close()
	}
	if ac.bridge != nil {
		ac.bridge.Close()
	}
}

// Shutdown ends all calls and releases resources for the session.
func (c *Controller) Shutdown() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.calls))
	for id := range c.calls {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		if ac, ok := c.get(id); ok {
			_ = ac.cm.EndCall(context.Background(), core.EndCallReasonUserEnded)
		}
		c.cleanup(id)
	}
}

func (c *Controller) get(callID string) (*activeCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ac, ok := c.calls[callID]
	return ac, ok
}

func (c *Controller) callFor(from types.JID, data *waBinary.Node) (*activeCall, bool) {
	callID := callIDFromNode(wrapCall(from, data))
	if callID == "" {
		return nil, false
	}
	return c.get(callID)
}

func (c *Controller) emitState(ci *call.CallInfo) {
	if c.emit == nil || ci == nil {
		return
	}
	dir := "outbound"
	if ci.Direction == core.CallDirectionIncoming {
		dir = "inbound"
	}
	c.emit(&CallStateEvent{
		CallID:    ci.CallID,
		Peer:      ci.PeerJid,
		Direction: dir,
		State:     string(ci.StateData.State),
		Reason:    string(ci.StateData.EndReason),
		Video:     ci.MediaType == core.CallMediaTypeVideo,
	})
}

func wrapCall(from types.JID, inner *waBinary.Node) *waBinary.Node {
	content := []waBinary.Node{}
	if inner != nil {
		content = append(content, *inner)
	}
	return &waBinary.Node{
		Tag:     "call",
		Attrs:   waBinary.Attrs{"from": from},
		Content: content,
	}
}

func callIDFromNode(node *waBinary.Node) string {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return ""
	}
	return info.CallID
}
