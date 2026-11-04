# bindings

Adapters from third party libraries to kui's interfaces. Each directory is a role, named after
what its packages provide; each package binds one library and is named `kui` plus the library,
so it never clashes with the library's own package name. Bindings depend on kui only, never on
each other. All live in this module: a library is compiled only into programs importing its
binding.

- [events](./events/): input producers writing to a `kui.EventWriter`, and the platform services
  of the same library that a UI asks for with `kui.Request`s: cursor shape, clipboard, text input.
  - [kuiglfw](./events/kuiglfw/): GLFW windows.
- [paint](./paint/): painters of `kui.DrawList`s on graphics APIs.
  - [kuigl](./paint/kuigl/): OpenGL 3.3 core, shaders written to the subset shared with OpenGL ES 3.0 and WebGL 2.
- [layout](./layout/): `kui.LayoutEngine`s.
  - [kuiglay](./layout/kuiglay/): [glay](https://github.com/soypat/glay), a Go port of clay.
- [fonts](./fonts/): font libraries: `kui.Font` metrics, text shaping (glyph selection, advances,
  kerning) and measuring, and `kui.FontFace`s rasterizing glyphs. One role, since font libraries
  usually do all of these.
  - [kuilefevre](./fonts/kuilefevre/): outline fonts such as TrueType with [lefevre](https://github.com/soypat/lefevre).
- [image](./image/): `kui.ImageSource`s decoding image formats.
