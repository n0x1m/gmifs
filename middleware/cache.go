package middleware

import (
	"sync"

	"github.com/n0x1m/gmifs/gemini"
)

type cache struct {
	sync.RWMutex
	documents map[string][]byte
	mimeTypes map[string]string

	tracker map[int]string
	index   int
	size    int
}

func (c *cache) Read(key string) ([]byte, string, bool) {
	c.RLock()
	doc, hitdoc := c.documents[key]
	mt, hitmime := c.mimeTypes[key]
	c.RUnlock()
	return doc, mt, hitdoc && hitmime
}

func (c *cache) housekeeping(key string) {
	// we enter locked and can modify
	if len(c.tracker) >= c.size {
		overflow := c.index
		expired := c.tracker[overflow]
		delete(c.documents, expired)
		delete(c.mimeTypes, expired)
		delete(c.tracker, overflow)
	}
	c.tracker[c.index] = key
	c.index++
	c.index = c.index % (c.size)
}

func (c *cache) Write(key string, mimeType string, doc []byte) {
	// protect against crashes when initialized and chained with zero size.
	if c.size <= 0 {
		return
	}
	c.Lock()
	c.housekeeping(key)
	c.documents[key] = doc
	c.mimeTypes[key] = mimeType
	c.Unlock()
}

func Cache(n int) func(next gemini.Handler) gemini.Handler {
	return (&cache{
		size:      n,
		documents: make(map[string][]byte, n+1),
		mimeTypes: make(map[string]string, n),
		tracker:   make(map[int]string, n),
	}).middleware
}

func (c *cache) middleware(next gemini.Handler) gemini.Handler {
	fn := func(w gemini.ResponseWriter, r *gemini.Request) {
		key := r.URL.Path
		if body, mimeType, hit := c.Read(key); hit {
			w.WriteHeader(gemini.StatusSuccess, mimeType)
			w.Write(body)
			return
		}

		ri := gemini.NewInterceptor(w)
		next.ServeGemini(ri, r)

		// only cache success responses
		if ri.HasHeader() && ri.Code == gemini.StatusSuccess {
			c.Write(key, ri.Meta, ri.Body.Bytes())
		}
		ri.Flush()
	}
	return gemini.HandlerFunc(fn)
}
