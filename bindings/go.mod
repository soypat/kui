module github.com/soypat/kui/bindings

go 1.26.3

require (
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260823155953-d41da22a9587
	github.com/soypat/glay v0.0.0-00010101000000-000000000000
	github.com/soypat/kui v0.0.0-20261103225723-a76fbb5d0c71
	github.com/soypat/lefevre v0.0.0-20260816213937-7ecc332044df
	golang.org/x/image v0.18.0
)

replace (
	// Local development against the checkouts of kui, which this module is nested in, and glay.
	github.com/soypat/glay => ../../glay
	github.com/soypat/kui => ../
	// Local lefevre for raster.RasterizeGlyph until it is published.
	github.com/soypat/lefevre => ../../../umyas/lefevre
)
