package kuiwgpu

// shader is the WGSL port of kuigl's GLSL shaders: it computes coverage and blending with the
// formulas and 8 bit rounding of kui-use's CPU painter (paint/soft), so both paint the same
// pixels, give or take a unit of blending rounding.
//
// The painter prefixes a line declaring the constant SRGB, true for sRGB targets: their
// stores encode the shader's output to sRGB, so it is decoded first and the bytes stored are
// the ones of the CPU painter. Blending then happens in linear light.
//
// The vertex shader places the quad of an instance from the vertex index: the instance buffer
// is the only vertex buffer.
const shader = `
struct Instance {
	@location(0) rect: vec4f,
	@location(1) quad: vec4f,
	@location(2) radii: vec4f,
	@location(3) param: vec4f,
	@location(4) color: vec4f,
	@location(5) kind: f32,
}

struct Varying {
	@builtin(position) pos: vec4f,
	@location(0) @interpolate(flat) rect: vec4f,
	@location(1) @interpolate(flat) radii: vec4f,
	@location(2) @interpolate(flat) param: vec4f,
	@location(3) @interpolate(flat) color: vec4f,
	@location(4) @interpolate(flat) kind: u32,
}

// fb.xy is the framebuffer size in pixels.
@group(0) @binding(0) var<uniform> fb: vec4f;
@group(0) @binding(1) var atlas: texture_2d<f32>;
@group(1) @binding(0) var image: texture_2d<f32>;

@vertex
fn vs_main(@builtin(vertex_index) vi: u32, in: Instance) -> Varying {
	let corner = vec2f(f32(vi & 1u), f32(vi >> 1u));
	let p = mix(in.quad.xy, in.quad.zw, corner);
	var out: Varying;
	out.pos = vec4f(2.0*p.x/fb.x - 1.0, 1.0 - 2.0*p.y/fb.y, 0.0, 1.0);
	out.rect = in.rect;
	out.radii = in.radii;
	out.param = in.param;
	out.color = in.color;
	out.kind = u32(in.kind);
	return out;
}

// overlap returns the length of the overlap of [a0, a1] with pixel [p, p+1].
fn overlap(p: f32, a0: f32, a1: f32) -> f32 { return max(min(p + 1.0, a1) - max(p, a0), 0.0); }

// rounded returns the coverage of pixel px, its top left corner, by rectangle r (x, y, w, h)
// with corner radii rad (top left, top right, bottom left, bottom right).
fn rounded(px: vec2f, r: vec4f, rad: vec4f) -> f32 {
	let cov = overlap(px.x, r.x, r.x + r.z) * overlap(px.y, r.y, r.y + r.w);
	if (cov == 0.0 || all(rad == vec4f(0.0))) {
		return cov;
	}
	let c = px + 0.5;
	let maxR = min(r.z, r.w) / 2.0;
	var R: f32;
	var o: vec2f;
	if (c.x < r.x + r.z/2.0 && c.y < r.y + r.w/2.0) {
		R = min(rad.x, maxR);
		o = vec2f(r.x + R, r.y + R);
		if (c.x >= o.x || c.y >= o.y) { return cov; }
	} else if (c.y < r.y + r.w/2.0) {
		R = min(rad.y, maxR);
		o = vec2f(r.x + r.z - R, r.y + R);
		if (c.x <= o.x || c.y >= o.y) { return cov; }
	} else if (c.x < r.x + r.z/2.0) {
		R = min(rad.z, maxR);
		o = vec2f(r.x + R, r.y + r.w - R);
		if (c.x >= o.x || c.y <= o.y) { return cov; }
	} else {
		R = min(rad.w, maxR);
		o = vec2f(r.x + r.z - R, r.y + r.w - R);
		if (c.x <= o.x || c.y <= o.y) { return cov; }
	}
	return min(cov, clamp(0.5 - (length(c - o) - R), 0.0, 1.0));
}

// q8 converts a coverage in [0, 1] to [0, 255].
fn q8(cov: f32) -> f32 { return floor(clamp(cov, 0.0, 1.0)*255.0 + 0.5); }

// u8 converts a normalized channel to [0, 255].
fn u8(v: f32) -> f32 { return floor(v*255.0 + 0.5); }

// mul8 returns a*b/255 rounded, of values in [0, 255].
fn mul8(a: f32, b: f32) -> f32 { return floor((a*b + 127.0) / 255.0); }

// texel returns the texel of n/d in [0, size), truncating like the CPU painter. GPUs divide by
// multiplying by an approximate reciprocal, which misses exact quotients by an ulp: near an
// integer k the quotient is settled by comparing n with k*d.
fn texel(n: f32, d: f32, size: i32) -> i32 {
	var q = n / d;
	let k = floor(q + 0.5);
	if (abs(q - k) < 1e-3) {
		q = select(k - 1.0, k, n >= k*d);
	}
	return clamp(i32(q), 0, size - 1);
}

// decodeSRGB decodes sRGB channels to linear light.
fn decodeSRGB(c: vec3f) -> vec3f {
	return select(pow((c + 0.055)/1.055, vec3f(2.4)), c/12.92, c <= vec3f(0.04045));
}

@fragment
fn fs_main(in: Varying) -> @location(0) vec4f {
	let px = floor(in.pos.xy); // Framebuffer coordinates have their origin at the top left.
	let c = px + 0.5;
	var rgb = vec3f(u8(in.color.r), u8(in.color.g), u8(in.color.b));
	var a = u8(in.color.a);
	var cov: f32;
	if (in.kind == 0u) { // Rectangle.
		cov = q8(rounded(px, in.rect, in.radii));
	} else if (in.kind == 1u) { // Border: param holds the left, right, top and bottom widths.
		let w = in.param;
		let inner = vec4f(in.rect.x + w.x, in.rect.y + w.z, in.rect.z - w.x - w.y, in.rect.w - w.w - w.z);
		let minW = min(min(w.x, w.y), min(w.z, w.w));
		var v = rounded(px, in.rect, in.radii);
		if (inner.z > 0.0 && inner.w > 0.0) {
			v -= rounded(px, inner, max(in.radii - minW, vec4f(0.0)));
		}
		cov = q8(v);
	} else if (in.kind == 2u) { // Circle inscribed in the rectangle.
		let center = in.rect.xy + in.rect.zw/2.0;
		cov = q8(0.5 - (length(c - center) - min(in.rect.z, in.rect.w)/2.0));
	} else if (in.kind == 3u) { // Line: rect holds start and extent, param.x the half width.
		let p = c - in.rect.xy;
		let d = in.rect.zw;
		let l2 = dot(d, d);
		var t = 0.0;
		if (l2 > 0.0) {
			t = clamp(dot(p, d)/l2, 0.0, 1.0);
		}
		cov = q8(0.5 - (length(p - t*d) - in.param.x));
	} else if (in.kind == 4u) { // Glyph: param holds the offset to atlas pixels.
		cov = u8(textureLoad(atlas, vec2i(px + in.param.xy), 0).r);
	} else { // Image, nearest sampled: param holds its size.
		let size = vec2i(in.param.xy);
		let sx = texel((c.x - in.rect.x)*in.param.x, in.rect.z, size.x);
		let sy = texel((c.y - in.rect.y)*in.param.y, in.rect.w, size.y);
		let s = textureLoad(image, vec2i(sx, sy), 0);
		rgb = vec3f(mul8(u8(s.r), rgb.r), mul8(u8(s.g), rgb.g), mul8(u8(s.b), rgb.b));
		a = mul8(u8(s.a), a);
		cov = q8(rounded(px, in.rect, in.radii));
	}
	let alpha = mul8(a, cov) / 255.0;
	var out = rgb/255.0*alpha; // Premultiplied.
	if (SRGB) {
		out = decodeSRGB(out);
	}
	return vec4f(out, alpha);
}
`
