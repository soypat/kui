package kuigl

import (
	"errors"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/soypat/kui"
)

// This file holds every GL call of the package, so other bindings (OpenGL ES, WebGL 2) can
// replace it.

const glslVersion = "#version 330 core\n"

// gpu holds the GL objects of a painter.
type gpu struct {
	prog          uint32
	vao, vbo      uint32
	atlas         uint32
	images        []uint32
	uFB           int32
	vboBytes      int
	atlasSide     int32
	width, height int
}

func (g *gpu) init(maxInstances, atlasSide, maxImages int) error {
	if err := gl.Init(); err != nil {
		return err
	}
	var maxTex int32
	gl.GetIntegerv(gl.MAX_TEXTURE_SIZE, &maxTex)
	if int32(atlasSide) > maxTex {
		return kui.ErrInvalidConfig
	}
	prog, err := link(glslVersion+vertexShader, glslVersion+fragmentShader)
	if err != nil {
		return err
	}
	*g = gpu{prog: prog, vboBytes: maxInstances * instanceSize, atlasSide: int32(atlasSide), images: make([]uint32, maxImages)}
	gl.UseProgram(prog)
	g.uFB = gl.GetUniformLocation(prog, gl.Str("uFB\x00"))
	gl.Uniform1i(gl.GetUniformLocation(prog, gl.Str("uAtlas\x00")), 0)
	gl.Uniform1i(gl.GetUniformLocation(prog, gl.Str("uImage\x00")), 1)

	gl.GenVertexArrays(1, &g.vao)
	gl.BindVertexArray(g.vao)
	gl.GenBuffers(1, &g.vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, g.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, g.vboBytes, nil, gl.STREAM_DRAW)
	for loc := uint32(0); loc < 6; loc++ {
		gl.EnableVertexAttribArray(loc)
		gl.VertexAttribDivisor(loc, 1)
	}

	gl.GenTextures(1, &g.atlas)
	newTexture(g.atlas)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.R8, g.atlasSide, g.atlasSide, 0, gl.RED, gl.UNSIGNED_BYTE, nil)
	gl.GenTextures(int32(len(g.images)), &g.images[0])
	for _, t := range g.images {
		newTexture(t)
	}
	if e := gl.GetError(); e != gl.NO_ERROR {
		g.close()
		return glError(e)
	}
	return nil
}

func newTexture(t uint32) {
	gl.BindTexture(gl.TEXTURE_2D, t)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
}

type glError uint32

func (e glError) Error() string { return "kuigl: GL error " + itoa(uint32(e)) }

func itoa(v uint32) string {
	var b [10]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + v%10)
		if v /= 10; v == 0 {
			return string(b[i:])
		}
	}
}

func link(vsrc, fsrc string) (uint32, error) {
	vs, err := compile(gl.VERTEX_SHADER, vsrc)
	if err != nil {
		return 0, err
	}
	defer gl.DeleteShader(vs)
	fs, err := compile(gl.FRAGMENT_SHADER, fsrc)
	if err != nil {
		return 0, err
	}
	defer gl.DeleteShader(fs)
	prog := gl.CreateProgram()
	gl.AttachShader(prog, vs)
	gl.AttachShader(prog, fs)
	gl.LinkProgram(prog)
	var status int32
	gl.GetProgramiv(prog, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var n int32
		gl.GetProgramiv(prog, gl.INFO_LOG_LENGTH, &n)
		log := make([]byte, n+1)
		gl.GetProgramInfoLog(prog, n, nil, &log[0])
		gl.DeleteProgram(prog)
		return 0, errors.New("kuigl: linking shaders: " + string(log[:n]))
	}
	return prog, nil
}

func compile(kind uint32, src string) (uint32, error) {
	s := gl.CreateShader(kind)
	csrc, free := gl.Strs(src + "\x00")
	gl.ShaderSource(s, 1, csrc, nil)
	free()
	gl.CompileShader(s)
	var status int32
	gl.GetShaderiv(s, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var n int32
		gl.GetShaderiv(s, gl.INFO_LOG_LENGTH, &n)
		log := make([]byte, n+1)
		gl.GetShaderInfoLog(s, n, nil, &log[0])
		gl.DeleteShader(s)
		return 0, errors.New("kuigl: compiling shader: " + string(log[:n]))
	}
	return s, nil
}

// begin sets the GL state for drawing into a framebuffer of width by height pixels.
func (g *gpu) begin(width, height int) {
	g.width, g.height = width, height
	gl.UseProgram(g.prog)
	gl.BindVertexArray(g.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, g.vbo)
	gl.Viewport(0, 0, int32(width), int32(height))
	gl.Uniform2f(g.uFB, float32(width), float32(height))
	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.STENCIL_TEST)
	gl.Disable(gl.SCISSOR_TEST)
	gl.Disable(gl.CULL_FACE)
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, g.atlas)
}

func (g *gpu) clear(c kui.Color) {
	gl.Disable(gl.SCISSOR_TEST)
	gl.ClearColor(float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255)
	gl.Clear(gl.COLOR_BUFFER_BIT)
}

// scissor clips drawing to clip, in pixels from the top left corner.
func (g *gpu) scissor(clip irect) {
	gl.Enable(gl.SCISSOR_TEST)
	gl.Scissor(clip.x0, int32(g.height)-clip.y1, clip.x1-clip.x0, clip.y1-clip.y0)
}

// draw uploads the glyphs and images of e's chunk and draws its batches, returning the number
// of draw calls.
func (g *gpu) draw(e *encoder) (draws int) {
	if d := e.atlas.dirty; !d.empty() {
		gl.ActiveTexture(gl.TEXTURE0)
		gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
		gl.PixelStorei(gl.UNPACK_ROW_LENGTH, g.atlasSide)
		gl.TexSubImage2D(gl.TEXTURE_2D, 0, d.x0, d.y0, d.x1-d.x0, d.y1-d.y0, gl.RED, gl.UNSIGNED_BYTE,
			unsafe.Pointer(&e.atlas.pix[int(d.y0)*e.atlas.side+int(d.x0)]))
		gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 0)
		gl.PixelStorei(gl.UNPACK_ALIGNMENT, 4)
		e.atlas.dirty = irect{}
	}
	bound := -1
	for i := range e.images {
		s := &e.images[i]
		if !s.upload {
			continue
		}
		s.upload = false
		pix, stride, w, h, ok := e.lookImage(s.id)
		if !ok || w != s.w || h != s.h {
			s.used = false // The source changed it since it was encoded.
			continue
		}
		gl.ActiveTexture(gl.TEXTURE1)
		gl.BindTexture(gl.TEXTURE_2D, g.images[i])
		bound = i
		gl.PixelStorei(gl.UNPACK_ROW_LENGTH, int32(stride/4))
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(h), 0, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&pix[0]))
		gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 0)
	}
	// Orphan the buffer so the driver need not wait for draws reading the previous chunk.
	gl.BufferData(gl.ARRAY_BUFFER, g.vboBytes, nil, gl.STREAM_DRAW)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(e.inst)*instanceSize, unsafe.Pointer(&e.inst[0]))
	for _, b := range e.batches {
		if b.count == 0 {
			continue
		}
		if b.image >= 0 && b.image != bound {
			gl.ActiveTexture(gl.TEXTURE1)
			gl.BindTexture(gl.TEXTURE_2D, g.images[b.image])
			bound = b.image
		}
		g.attribs(b.first)
		gl.DrawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, int32(b.count))
		draws++
	}
	return draws
}

// attribs points the instance attributes at the instances from first on. GL 3.3 and ES 3.0
// lack a base instance, so batches move the pointers instead.
func (g *gpu) attribs(first int) {
	var in instance
	base := uintptr(first * instanceSize)
	const stride = int32(instanceSize)
	gl.VertexAttribPointerWithOffset(0, 4, gl.FLOAT, false, stride, base+unsafe.Offsetof(in.Rect))
	gl.VertexAttribPointerWithOffset(1, 4, gl.FLOAT, false, stride, base+unsafe.Offsetof(in.Quad))
	gl.VertexAttribPointerWithOffset(2, 4, gl.FLOAT, false, stride, base+unsafe.Offsetof(in.Radii))
	gl.VertexAttribPointerWithOffset(3, 4, gl.FLOAT, false, stride, base+unsafe.Offsetof(in.Param))
	gl.VertexAttribPointerWithOffset(4, 4, gl.UNSIGNED_BYTE, true, stride, base+unsafe.Offsetof(in.Color))
	gl.VertexAttribPointerWithOffset(5, 1, gl.FLOAT, false, stride, base+unsafe.Offsetof(in.Kind))
}

func (g *gpu) close() {
	gl.DeleteProgram(g.prog)
	gl.DeleteBuffers(1, &g.vbo)
	gl.DeleteVertexArrays(1, &g.vao)
	gl.DeleteTextures(1, &g.atlas)
	if len(g.images) > 0 {
		gl.DeleteTextures(int32(len(g.images)), &g.images[0])
	}
	*g = gpu{}
}
