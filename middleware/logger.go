package middleware

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/n0x1m/gmifs/gemini"
)

func Logger(log *log.Logger, prefix string) func(gemini.Handler) gemini.Handler {
	return func(next gemini.Handler) gemini.Handler {
		fn := func(w gemini.ResponseWriter, r *gemini.Request) {
			t := time.Now()

			ri := gemini.NewInterceptor(w)
			next.ServeGemini(ri, r)
			ri.Flush()

			ip := strings.Split(r.RemoteAddr, ":")[0]
			fmt.Fprintf(log.Writer(), "%s%s - - [%s] \"%s\" %d %d - %v\n",
				prefix,
				ip,
				t.Format("02/Jan/2006:15:04:05 -0700"),
				r.URL.Path,
				ri.Code,
				ri.Body.Len(),
				time.Since(t),
			)
		}
		return gemini.HandlerFunc(fn)
	}
}
