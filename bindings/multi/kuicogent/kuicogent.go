// Package kuicogent binds Cogent Core (cogentcore.org/core) to kui, filling several roles at
// once:
//   - [Input] translates the events of a Cogent [system.Window] to kui events.
//   - [Painter] paints kui draw lists with Cogent's [paint.Painter]; [Present] shows the
//     result on a window.
//   - [Measurer] measures text with Cogent's shaper, the way [Painter] draws it, for kui
//     layout engines.
//
// It is a minimal translation. Cogent queues events itself, merging moves and scrolls, so
// Input does not coalesce. Text uses Cogent's default font family for every kui font, at the
// size of the command, without letter spacing. Not handled: touch input, input methods, kui
// requests (cursor shape, clipboard), image tints, pointer leave (Cogent's desktop driver does
// not report it). Unlike most kui implementations, painting and measuring allocate: Cogent
// shapes text and records renders on the heap.
//
// Cogent Core requires Go 1.27, so this package is a module of its own.
package kuicogent
