package llm

type EventHandler interface {
	Handle(e Event)
}

type EventHandlerFunc func(e Event)

func (h EventHandlerFunc) Handle(e Event) { h(e) }

type TypedEventHandler[T any] func(e T)

func (h TypedEventHandler[T]) Handle(e Event) {
	ee, ok := e.(T)
	if !ok {
		return
	}
	h(ee)
}
