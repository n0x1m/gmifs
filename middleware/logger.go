package middleware

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/n0x1m/gmifs/gemini"
)

func Logger(log *log.Logger) func(next gemini.Handler) gemini.Handler {
	return func(next gemini.Handler) gemini.Handler {
		fn := func(w io.Writer, r *gemini.Request) {
			t := time.Now()

			next.ServeGemini(w, r)

			ip := strings.Split(r.RemoteAddr, ":")[0]
			hostname, _ := os.Hostname()
			fmt.Fprintf(log.Writer(), "%s %s - - [%s] \"%s\" - %v\n",
				hostname,
				ip,
				t.Format("02/Jan/2006:15:04:05 -0700"),
				r.URL.Path,
				time.Since(t),
			)
		}
		return gemini.HandlerFunc(fn)
	}
}
