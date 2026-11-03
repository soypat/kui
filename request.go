package kui

// Cursor is a pointer cursor shape requested from the host.
type Cursor uint8

const (
	CursorDefault Cursor = iota // default
	// CursorPointer is a hand, for clickable elements.
	CursorPointer // pointer
	// CursorText is an I-beam, for text input.
	CursorText   // text
	CursorGrab   // grab
	CursorResize // resize
)

// RequestKind is the kind of a [Request].
type RequestKind uint8

const (
	// RequestCursor asks the host to change the pointer cursor to Cursor.
	RequestCursor RequestKind = iota + 1 // cursor
	// RequestTextInput asks the host to start (Code 1) or stop (Code 0) text input for Node,
	// i.e. show or hide a soft keyboard. Rect is the area of the input to keep visible, i.e.
	// where to place an input method's candidate window.
	RequestTextInput // text-input
	// RequestClipboardWrite asks the host to copy the request's text to the clipboard.
	RequestClipboardWrite // clipboard-write
	// RequestClipboardRead asks the host to paste: deliver the clipboard's contents as an
	// [EventText] event.
	RequestClipboardRead // clipboard-read
)

// Request is a request from a UI to its host platform, the counterpart of input [Event]s.
// It contains no pointers: its text, if any, is held in a buffer passed along with it.
type Request struct {
	Kind   RequestKind
	Cursor Cursor
	// Node is the node the request concerns, i.e. the focused text input.
	Node Handle
	// Code is a request specific value, see [RequestKind].
	Code uint32
	Rect Rect
	// Text is the byte range of the request's text in the buffer passed along with it.
	Text Span
}
