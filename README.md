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


## kui2go

Package [`kui2go`](./kui2go/) contains implementations that are simple and straightforward enough to be provided inside of kui.
- [kui2go.EventSPSC](./kui2go/event-spsc.go): Single-Producer/Single-Consumer event queue, implements EventWriter and EventReader.
- [kui2go.TextFaces](./kui2go/text-faces.go): Measurer over FontFaces indexed by font, measuring with the advances painters draw with.
- [kuiscene.Scene](./kui2go/kuiscene/scene.go): retained node tree with layout, hit testing, gesture routing, focus, timers, scrolling, text editing and painting.

## bindings

Module [`bindings`](./bindings/) adapts third party libraries to kui's interfaces, depending only on kui. See README in directory.

## gesture
Package [`gesture`](./gesture/) contains gesture processing logic. Omitted from `kui` since its processing is

## Fuzzing
```
go test ./kui2go/kuiscene -run TestWidgetLaws -states 20000 -seed 7
go test ./kui2go -run TestLayoutBasicRandom -trees 50000
go test ./kui2go/kuiscene -fuzz FuzzWidgets -fuzztime 60s
```