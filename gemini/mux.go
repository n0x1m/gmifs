package gemini

import "io"

// Middlewares type is a slice of gemini middleware handlers.
type Middleware func(Handler) Handler

type Mux struct {
	middlewares []Middleware
	handler     Handler
}

func NewMux() *Mux {
	return &Mux{}
}

// Use appends a handler to the Mux handler stack.
func (m *Mux) Use(handlers ...Middleware) {
	m.middlewares = append(m.middlewares, handlers...)
}

func (m *Mux) Handle(endpoint Handler) Handler {
	m.handler = chain(m.middlewares, endpoint)
	return m.handler
}

func (m *Mux) ServeGemini(w io.Writer, r *Request) {
	m.handler.ServeGemini(w, r)
}

// chain builds a Handler composed of an inline middleware stack and endpoint
// handler in the order they are passed.
func chain(middlewares []Middleware, endpoint Handler) Handler {
	// Return ahead of time if there aren't any middlewares for the chain
	if len(middlewares) == 0 {
		return endpoint
	}

	// Wrap the end handler with the middleware chain
	h := middlewares[len(middlewares)-1](endpoint)
	for i := len(middlewares) - 2; i >= 0; i-- {
		h = middlewares[i](h)
	}

	return h
}
