package llm

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/codewandler/llm/tool"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

type eventPub struct {
	id        string
	seq       uint64
	createdAt time.Time
	ch        chan Envelope
	closeOnce sync.Once
}

func NewEventPublisher() (Publisher, <-chan Envelope) {
	ch := make(chan Envelope, 64)
	s := &eventPub{
		id:        gonanoid.Must(),
		createdAt: time.Now(),
		ch:        ch,
	}
	s.Publish(&StreamCreatedEvent{})
	return s, ch
}

func createEnvelope(s *eventPub, payload Event) Envelope {
	return Envelope{
		Type: payload.Type(),
		Data: payload,
		Meta: EventMeta{
			Seq:       atomic.AddUint64(&s.seq, 1),
			CreatedAt: time.Now(),
			After:     time.Now().Sub(s.createdAt),
			RequestID: s.id,
		},
	}
}
func (s *eventPub) publish(e Envelope)    { s.ch <- e }
func (s *eventPub) Publish(payload Event) { s.publish(createEnvelope(s, payload)) }
func (s *eventPub) Close() {
	s.closeOnce.Do(func() {
		close(s.ch)
	})
}
func (s *eventPub) Started(started StreamStartedEvent) {
	s.Publish(&StreamStartedEvent{
		RequestID: started.RequestID,
		Model:     started.Model,
	})
}
func (s *eventPub) Debug(msg string, data any) {
	s.Publish(&DebugEvent{Message: msg, Data: data})
}
func (s *eventPub) Routed(routed RouteInfo)            { s.Publish(&RouteInfoEvent{RouteInfo: routed}) }
func (s *eventPub) Delta(d *DeltaEvent)                { s.Publish(d) }
func (s *eventPub) Usage(usage Usage)                  { s.Publish(&UsageUpdatedEvent{Usage: usage}) }
func (s *eventPub) Completed(completed CompletedEvent) { s.Publish(&completed) }
func (s *eventPub) Error(err error)                    { s.Publish(&ErrorEvent{Error: err}) }
func (s *eventPub) ToolCall(tc tool.Call)              { s.Publish(&ToolCallEvent{ToolCall: tc}) }
