# kui

An ecosystem for building UI frameworks that can run anywhere.

kui has minimal logic, instead needing the user to plug in interfaces that are needed for UI frameworks to work like:

- [Font](./text.go): typeface metrics in font units: character map and glyph advances.
- [FontFace](./text.go): font rasterizing for CPU drawing, yields glyph alpha masks and line metrics.
- [Measurer](./text.go): text measuring so layout can size text before it is drawn.
- [LayoutEngine](./layout.go): computes node geometry of a [Tree](./layout.go) within a viewport.
- [EventWriter](./events.go): producer end of an event queue, written to by platform input drivers.
- [EventReader](./events.go): consumer end of an event queue, drained by the UI to process input [Events](./events.go).
- [EventBuilder](./events.go): Not an interface per-se, but it is the contact point with devices such as keyboards, touch pads and mice.