package llm

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// errSSEStop signals readSSE to terminate cleanly without returning an error.
var errSSEStop = errors.New("sse stop")

// readSSE parses a Server-Sent Events stream using bufio.Reader (no 64KB line limit).
// Comments (lines starting with ':') are ignored; multi-line data fields are joined with '\n'.
func readSSE(ctx context.Context, r io.Reader, handle func(event, data string) error) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var eventName string
	var dataLines []string

	dispatch := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		ev := eventName
		data := strings.Join(dataLines, "\n")
		eventName = ""
		dataLines = dataLines[:0]
		return handle(ev, data)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := br.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if line == "" {
				if err := dispatch(); err != nil {
					if errors.Is(err, errSSEStop) {
						return nil
					}
					return err
				}
			} else if !strings.HasPrefix(line, ":") {
				field, val, hasColon := strings.Cut(line, ":")
				if hasColon {
					val = strings.TrimPrefix(val, " ")
					switch field {
					case "event":
						eventName = val
					case "data":
						dataLines = append(dataLines, val)
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			if err := dispatch(); err != nil && !errors.Is(err, errSSEStop) {
				return err
			}
			return nil
		}
	}
}
