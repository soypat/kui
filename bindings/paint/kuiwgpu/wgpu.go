package kuiwgpu

import (
	"math"
	"unsafe"

	"github.com/oliverbestmann/webgpu/wgpu"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go/kuibatch"
)

// This file holds every WebGPU call of the package.
//
// Each chunk of the encoder is drawn by a command buffer of its own, submitted before the
// next chunk is uploaded: queue writes take effect before the next submit, so chunks sharing
// a command buffer would draw with the instances, glyphs and images of the last one.

// imageTexture is an image texture and the bind group sampling it.
type imageTexture struct {
	tex   *wgpu.Texture
	view  *wgpu.TextureView
	group *wgpu.BindGroup
	w, h  int
}

func (t *imageTexture) release() {
	release(t.group)
	release(t.view)
	release(t.tex)
	*t = imageTexture{}
}

// gpu holds the WebGPU objects of a painter and the state of the frame being painted.
type gpu struct {
	device         *wgpu.Device
	queue          *wgpu.Queue
	ownQueue       bool // The queue was got from the device and must be released.
	srgb           bool
	maxTexture     int
	module         *wgpu.ShaderModule
	globalsLayout  *wgpu.BindGroupLayout
	imageLayout    *wgpu.BindGroupLayout
	pipelineLayout *wgpu.PipelineLayout
	pipeline       *wgpu.RenderPipeline
	instances      *wgpu.Buffer
	uniforms       *wgpu.Buffer
	atlas          *wgpu.Texture
	atlasView      *wgpu.TextureView
	atlasSide      int
	globals        *wgpu.BindGroup
	blank          imageTexture // Transparent pixel bound for batches sampling no image.
	images         []imageTexture
	fb             [4]float32 // Framebuffer size in the uniforms.

	target     *wgpu.TextureView
	clear      bool // The next pass clears the target.
	clearColor kui.Color
	submits    int
	err        error // First error of the frame.
}

func (g *gpu) init(device *wgpu.Device, queue *wgpu.Queue, format wgpu.TextureFormat, maxInstances, atlasSide, maxImages int) (err error) {
	*g = gpu{device: device, queue: queue, atlasSide: atlasSide, images: make([]imageTexture, maxImages)}
	defer func() {
		if err != nil {
			g.close()
		}
	}()
	g.maxTexture = int(device.GetLimits().MaxTextureDimension2D)
	if atlasSide > g.maxTexture {
		return kui.ErrInvalidConfig
	}
	if g.queue == nil {
		g.queue, g.ownQueue = device.GetQueue(), true
	}
	g.srgb = format == wgpu.TextureFormatRGBA8UnormSrgb || format == wgpu.TextureFormatBGRA8UnormSrgb
	src := "const SRGB = false;\n" + shader
	if g.srgb {
		src = "const SRGB = true;\n" + shader
	}
	if g.module, err = device.TryCreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:      "kuiwgpu",
		WGSLSource: &wgpu.ShaderSourceWGSL{Code: src},
	}); err != nil {
		return err
	}
	texture := wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeUnfilterableFloat, ViewDimension: wgpu.TextureViewDimension2D}
	if g.globalsLayout, err = device.TryCreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "kuiwgpu globals",
		Entries: []wgpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 16}},
			{Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: texture},
		},
	}); err != nil {
		return err
	}
	if g.imageLayout, err = device.TryCreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "kuiwgpu image",
		Entries: []wgpu.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: texture}},
	}); err != nil {
		return err
	}
	if g.pipelineLayout, err = device.TryCreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "kuiwgpu",
		BindGroupLayouts: []*wgpu.BindGroupLayout{g.globalsLayout, g.imageLayout},
	}); err != nil {
		return err
	}
	var in kuibatch.Instance
	premultiplied := wgpu.BlendComponent{Operation: wgpu.BlendOperationAdd, SrcFactor: wgpu.BlendFactorOne, DstFactor: wgpu.BlendFactorOneMinusSrcAlpha}
	if g.pipeline, err = device.TryCreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "kuiwgpu",
		Layout: g.pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     g.module,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: uint64(kuibatch.InstanceSize),
				StepMode:    wgpu.VertexStepModeInstance,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x4, Offset: uint64(unsafe.Offsetof(in.Rect)), ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x4, Offset: uint64(unsafe.Offsetof(in.Quad)), ShaderLocation: 1},
					{Format: wgpu.VertexFormatFloat32x4, Offset: uint64(unsafe.Offsetof(in.Radii)), ShaderLocation: 2},
					{Format: wgpu.VertexFormatFloat32x4, Offset: uint64(unsafe.Offsetof(in.Param)), ShaderLocation: 3},
					{Format: wgpu.VertexFormatUnorm8x4, Offset: uint64(unsafe.Offsetof(in.Color)), ShaderLocation: 4},
					{Format: wgpu.VertexFormatFloat32, Offset: uint64(unsafe.Offsetof(in.Kind)), ShaderLocation: 5},
				},
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleStrip,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     g.module,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    format,
				Blend:     &wgpu.BlendState{Color: premultiplied, Alpha: premultiplied},
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
	}); err != nil {
		return err
	}
	if g.instances, err = device.TryCreateBuffer(&wgpu.BufferDescriptor{
		Label: "kuiwgpu instances",
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		Size:  uint64(maxInstances * kuibatch.InstanceSize),
	}); err != nil {
		return err
	}
	if g.uniforms, err = device.TryCreateBuffer(&wgpu.BufferDescriptor{
		Label: "kuiwgpu uniforms",
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		Size:  16,
	}); err != nil {
		return err
	}
	if g.atlas, g.atlasView, err = g.newTexture("kuiwgpu atlas", wgpu.TextureFormatR8Unorm, atlasSide, atlasSide); err != nil {
		return err
	}
	if g.globals, err = device.TryCreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "kuiwgpu globals",
		Layout: g.globalsLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniforms, Size: 16},
			{Binding: 1, TextureView: g.atlasView},
		},
	}); err != nil {
		return err
	}
	if err = g.newImage(&g.blank, 1, 1); err != nil {
		return err
	}
	return g.queue.TryWriteTexture(&wgpu.TexelCopyTextureInfo{Texture: g.blank.tex, Aspect: wgpu.TextureAspectAll},
		make([]byte, 4), &wgpu.TexelCopyBufferLayout{BytesPerRow: 4, RowsPerImage: 1}, &wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1})
}

// newTexture creates a w by h texture to upload to and sample.
func (g *gpu) newTexture(label string, format wgpu.TextureFormat, w, h int) (*wgpu.Texture, *wgpu.TextureView, error) {
	tex, err := g.device.TryCreateTexture(&wgpu.TextureDescriptor{
		Label:         label,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:        format,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		return nil, nil, err
	}
	view, err := tex.TryCreateView(nil)
	if err != nil {
		tex.Release()
		return nil, nil, err
	}
	return tex, view, nil
}

// newImage replaces t with a w by h image texture.
func (g *gpu) newImage(t *imageTexture, w, h int) error {
	t.release()
	tex, view, err := g.newTexture("kuiwgpu image", wgpu.TextureFormatRGBA8Unorm, w, h)
	if err != nil {
		return err
	}
	group, err := g.device.TryCreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:   "kuiwgpu image",
		Layout:  g.imageLayout,
		Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}},
	})
	if err != nil {
		view.Release()
		tex.Release()
		return err
	}
	*t = imageTexture{tex: tex, view: view, group: group, w: w, h: h}
	return nil
}

func (g *gpu) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}

// begin starts painting target, of width by height pixels.
func (g *gpu) begin(target *wgpu.TextureView, width, height int) {
	g.target = target
	fb := [4]float32{float32(width), float32(height)}
	if fb == g.fb {
		return
	}
	g.fb = fb
	if err := g.queue.TryWriteBuffer(g.uniforms, 0, unsafe.Slice((*byte)(unsafe.Pointer(&g.fb[0])), 16)); err != nil {
		g.fail(err)
	}
}

// beginPass begins a render pass on the target, clearing it first if a clear is pending. It
// returns nil on failure.
func (g *gpu) beginPass() (*wgpu.RenderPassEncoder, *wgpu.CommandEncoder) {
	enc, err := g.device.TryCreateCommandEncoder(nil)
	if err != nil {
		g.fail(err)
		return nil, nil
	}
	att := wgpu.RenderPassColorAttachment{View: g.target, LoadOp: wgpu.LoadOpLoad, StoreOp: wgpu.StoreOpStore}
	if g.clear {
		g.clear = false
		c := g.clearColor
		att.LoadOp = wgpu.LoadOpClear
		att.ClearValue = wgpu.Color{R: g.channel(c.R), G: g.channel(c.G), B: g.channel(c.B), A: float64(c.A) / 255}
	}
	return enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{att}}), enc
}

// channel returns the clear value of color channel v, decoded from sRGB for sRGB targets.
func (g *gpu) channel(v uint8) float64 {
	c := float64(v) / 255
	if !g.srgb {
		return c
	}
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// endPass ends pass and submits the commands of enc.
func (g *gpu) endPass(pass *wgpu.RenderPassEncoder, enc *wgpu.CommandEncoder) {
	if pass == nil {
		return
	}
	defer enc.Release()
	err := pass.TryEnd()
	pass.Release()
	if err != nil {
		g.fail(err)
		return
	}
	cmds, err := enc.TryFinish(nil)
	if err != nil {
		g.fail(err)
		return
	}
	g.queue.Submit(cmds)
	cmds.Release()
	g.submits++
}

// draw uploads the glyphs and images of e's chunk and draws its batches, returning the number
// of draw calls.
func (g *gpu) draw(e *kuibatch.Encoder) (draws int) {
	if r, pix := e.AtlasUpload(); !r.Empty() && g.err == nil {
		side := g.atlasSide
		w, h := r.X1-r.X0, r.Y1-r.Y0
		data := pix[int(r.Y0)*side+int(r.X0) : int(r.Y1-1)*side+int(r.X1)]
		if err := g.queue.TryWriteTexture(
			&wgpu.TexelCopyTextureInfo{Texture: g.atlas, Origin: wgpu.Origin3D{X: uint32(r.X0), Y: uint32(r.Y0)}, Aspect: wgpu.TextureAspectAll},
			data, &wgpu.TexelCopyBufferLayout{BytesPerRow: uint32(side), RowsPerImage: uint32(h)},
			&wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		); err != nil {
			g.fail(err)
		}
	}
	for {
		slot, pix, stride, w, h, ok := e.ImageUpload()
		if !ok {
			break
		}
		g.uploadImage(&g.images[slot], pix, stride, w, h)
	}
	inst := e.Instances()
	if g.err != nil || len(inst) == 0 {
		return 0
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&inst[0])), len(inst)*kuibatch.InstanceSize)
	if err := g.queue.TryWriteBuffer(g.instances, 0, data); err != nil {
		g.fail(err)
		return 0
	}
	pass, enc := g.beginPass()
	if pass == nil {
		return 0
	}
	pass.SetPipeline(g.pipeline)
	pass.SetBindGroup(0, g.globals, nil)
	pass.SetBindGroup(1, g.blank.group, nil)
	pass.SetVertexBuffer(0, g.instances, 0, uint64(len(data)))
	bound := -1
	for _, b := range e.Batches() {
		if b.Count == 0 {
			continue
		}
		if b.Image >= 0 && b.Image != bound {
			group := g.blank.group // The image could not be uploaded.
			if t := &g.images[b.Image]; t.group != nil {
				group = t.group
			}
			pass.SetBindGroup(1, group, nil)
			bound = b.Image
		}
		pass.Draw(4, uint32(b.Count), 0, uint32(b.First))
		draws++
	}
	g.endPass(pass, enc)
	return draws
}

// uploadImage uploads the pixels of an image to t, made the image's size. Images larger than
// the device's textures are dropped, sampling the blank texture instead.
func (g *gpu) uploadImage(t *imageTexture, pix []byte, stride, w, h int) {
	if g.err != nil {
		return
	}
	if w > g.maxTexture || h > g.maxTexture {
		t.release()
		return
	}
	if t.tex == nil || t.w != w || t.h != h {
		if err := g.newImage(t, w, h); err != nil {
			g.fail(err)
			return
		}
	}
	if err := g.queue.TryWriteTexture(
		&wgpu.TexelCopyTextureInfo{Texture: t.tex, Aspect: wgpu.TextureAspectAll},
		pix[:(h-1)*stride+4*w], &wgpu.TexelCopyBufferLayout{BytesPerRow: uint32(stride), RowsPerImage: uint32(h)},
		&wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
	); err != nil {
		g.fail(err)
	}
}

func (g *gpu) close() {
	for i := range g.images {
		g.images[i].release()
	}
	g.blank.release()
	release(g.globals)
	release(g.atlasView)
	release(g.atlas)
	release(g.uniforms)
	release(g.instances)
	release(g.pipeline)
	release(g.pipelineLayout)
	release(g.imageLayout)
	release(g.globalsLayout)
	release(g.module)
	if g.ownQueue {
		release(g.queue)
	}
	*g = gpu{}
}

// release releases WebGPU object p, if not nil.
func release[T any, P interface {
	*T
	Release()
}](p P) {
	if p != nil {
		p.Release()
	}
}
