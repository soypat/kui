# kui

An ecosystem for building UI frameworks that can run anywhere.

kui has minimal logic, instead needing the user to plug in interfaces that are needed for UI frameworks to work like:

- [Font](./text.go): typeface metrics in font units: character map and glyph advances.
- [FontFace](./text.go): font rasterizing for CPU drawing, yields glyph alpha masks and line metrics.
- [FontMeasurer](./text.go): text measuring so layout can size text before it is drawn.
- [LayoutEngine](./layout.go): computes node geometry of a [Tree](./layout.go) within a viewport.
- [EventWriter](./events.go): producer end of an event queue, written to by platform input drivers.
- [EventReader](./events.go): consumer end of an event queue, drained by the UI to process input [Events](./events.go).
- [EventBuilder](./events.go): Not an interface per-se, but it is the contact point with devices such as keyboards, touch pads and mice.

## webgpu example
GLFW window and events, WebGPU painting, lefevre text shaping:
```
cd bindings
go run ./paint/kuiwgpu/examples/window
```

## cogent-core example
```
cd bindings/multi/kuicogent/examples/window
go run .
```
![kui on cogent core](https://private-user-images.githubusercontent.com/26156425/694302119-60ce6ffe-6e9b-4494-9955-372caac2ce68.png?jwt=eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJnaXRodWIuY29tIiwiYXVkIjoicmF3LmdpdGh1YnVzZXJjb250ZW50LmNvbSIsImtleSI6ImtleTUiLCJleHAiOjE3OTQ1NzMxNjksIm5iZiI6MTc5NDU3Mjg2OSwicGF0aCI6Ii8yNjE1NjQyNS82OTQzMDIxMTktNjBjZTZmZmUtNmU5Yi00NDk0LTk5NTUtMzcyY2FhYzJjZTY4LnBuZz9YLUFtei1BbGdvcml0aG09QVdTNC1ITUFDLVNIQTI1NiZYLUFtei1DcmVkZW50aWFsPUFLSUFWQ09EWUxTQTUzUFFLNFpBJTJGMjAyNjExMTMlMkZ1cy1lYXN0LTElMkZzMyUyRmF3czRfcmVxdWVzdCZYLUFtei1EYXRlPTIwMjYxMTEzVDEyMjc0OVomWC1BbXotRXhwaXJlcz0zMDAmWC1BbXotU2lnbmF0dXJlPTgxYTJiYWIyMzRlYzQyYzJlNjNlMjY0NTMwMDYzY2QzOWM5MWZhMjdkNWExNWZmZWQ4ZDE3YTBhMzkzNzE3NjQmWC1BbXotU2lnbmVkSGVhZGVycz1ob3N0JnJlc3BvbnNlLWNvbnRlbnQtdHlwZT1pbWFnZSUyRnBuZyJ9.s10K5t9A7t-To7i4JU9XAEdmbaf9_b4pB3QvgCZdRzY)


## kui2go

Package [`kui2go`](./kui2go/) contains implementations that are simple and straightforward enough to be provided inside of kui.
- [kui2go.EventSPSC](./kui2go/event-spsc.go): Single-Producer/Single-Consumer event queue, implements EventWriter and EventReader.
- [kui2go.TextFaces](./kui2go/text-faces.go): FontMeasurer over FontFaces indexed by font, measuring with the advances painters draw with.
- [kuiscene.Scene](./kui2go/kuiscene/scene.go): retained node tree with layout, hit testing, gesture routing, focus, timers, scrolling, text editing and painting.
- [kuibatch.Encoder](./kui2go/kuibatch/encode.go): batches draw lists into instanced quads and a glyph atlas for GPU painters, whatever their graphics API.
- [kui2go.LayoutBasic](./kui2go/layout-basic.go): A minimal layout engine that has been fuzzed against glay, which itself has been fuzzed against clayui. Has minor differences in layout with glay.

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