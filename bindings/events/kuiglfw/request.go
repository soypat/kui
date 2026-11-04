package kuiglfw

import (
	"unsafe"

	"github.com/soypat/kui"
)

// Apply carries out the requests of a UI step: it sets the cursor shape and writes or reads
// the clipboard. The text of request r is text[r.Text.Start:r.Text.End()]. Clipboard
// contents read arrive as a [kui.EventText] written after the events already written;
// reading allocates, in GLFW. [kui.RequestTextInput] is ignored: GLFW has no input method
// support.
func (in *Input) Apply(reqs []kui.Request, text []byte) {
	for i := range reqs {
		switch r := &reqs[i]; r.Kind {
		case kui.RequestCursor:
			in.setCursor(r.Cursor)
		case kui.RequestClipboardWrite:
			if r.Text.Len > 0 && uint64(r.Text.End()) <= uint64(len(text)) {
				in.win.SetClipboardString(unsafe.String(&text[r.Text.Start], int(r.Text.Len)))
			}
		case kui.RequestClipboardRead:
			if s := in.win.GetClipboardString(); s != "" {
				in.Flush()
				if in.w.WriteTextEvent(in.events.NewEvent(kui.EventText), unsafe.Slice(unsafe.StringData(s), len(s))) != nil {
					in.dropped++
				}
			}
		}
	}
}

func (in *Input) setCursor(c kui.Cursor) {
	if int(c) >= numCursors || c == in.cursor {
		return
	}
	in.cursor = c
	in.win.SetCursor(in.cursors[c])
}
