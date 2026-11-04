module github.com/soypat/kui/bindings

go 1.26.3

require (
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71
	github.com/go-gl/glfw/v3.4/glfw v0.1.0-pre.1.0.20260628091122-0bd588dc30cf
	github.com/soypat/glay v0.0.0-00010101000000-000000000000
	github.com/soypat/kui v0.0.0-20261103225723-a76fbb5d0c71
	github.com/soypat/lefevre v0.0.0-20260816213937-7ecc332044df
	golang.org/x/image v0.18.0
)

require (
	github.com/oliverbestmann/webgpu v1.37.0
	github.com/oliverbestmann/webgpu/libs-android v0.0.0-20260628152806-6b27e30a172e // indirect
	github.com/oliverbestmann/webgpu/libs-darwin v0.0.0-20260628152755-66a5dfa57f8d // indirect
	github.com/oliverbestmann/webgpu/libs-ios v0.0.0-20260628152757-fe2537e7ddac // indirect
	github.com/oliverbestmann/webgpu/libs-linux v0.0.0-20260628152803-421b8a341d08 // indirect
	github.com/oliverbestmann/webgpu/libs-windows v0.0.0-20260628152801-f47d1b682eb8 // indirect
)

replace (
	// Local development against the checkouts of kui, which this module is nested in, and glay.
	github.com/soypat/glay => ../../glay
	github.com/soypat/kui => ../
	// Local lefevre for raster.RasterizeGlyph until it is published.
	github.com/soypat/lefevre => ../../../umyas/lefevre
)
