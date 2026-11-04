package kuigl

// The shaders are written to the common subset of GLSL 3.30 core and GLSL ES 3.00 (OpenGL ES
// 3.0, WebGL 2): the version line is prefixed by the GL binding.
//
// The fragment shader computes coverage and blending with the formulas and 8 bit rounding of
// kui-use's CPU painter (paint/soft), so both paint the same pixels, give or take a unit of
// blending rounding.

// vertexShader places the quad of an instance, from gl_VertexID: no vertex buffer is needed.
const vertexShader = `
layout(location = 0) in vec4 aRect;
layout(location = 1) in vec4 aQuad;
layout(location = 2) in vec4 aRadii;
layout(location = 3) in vec4 aParam;
layout(location = 4) in vec4 aColor;
layout(location = 5) in float aKind;

uniform vec2 uFB;

flat out vec4 vRect;
flat out vec4 vRadii;
flat out vec4 vParam;
flat out vec4 vColor;
flat out int vKind;

void main() {
	vec2 corner = vec2(float(gl_VertexID & 1), float(gl_VertexID >> 1));
	vec2 p = mix(aQuad.xy, aQuad.zw, corner);
	gl_Position = vec4(2.0*p.x/uFB.x - 1.0, 1.0 - 2.0*p.y/uFB.y, 0.0, 1.0);
	vRect = aRect;
	vRadii = aRadii;
	vParam = aParam;
	vColor = aColor;
	vKind = int(aKind);
}
`

const fragmentShader = `
uniform vec2 uFB;
uniform sampler2D uAtlas;
uniform sampler2D uImage;

flat in vec4 vRect;
flat in vec4 vRadii;
flat in vec4 vParam;
flat in vec4 vColor;
flat in int vKind;

out vec4 fragColor;

// overlap returns the length of the overlap of [a0, a1] with pixel [p, p+1].
float overlap(float p, float a0, float a1) { return max(min(p + 1.0, a1) - max(p, a0), 0.0); }

// rounded returns the coverage of pixel px, its top left corner, by rectangle r (x, y, w, h)
// with corner radii rad (top left, top right, bottom left, bottom right).
float rounded(vec2 px, vec4 r, vec4 rad) {
	float cov = overlap(px.x, r.x, r.x + r.z) * overlap(px.y, r.y, r.y + r.w);
	if (cov == 0.0 || rad == vec4(0.0)) {
		return cov;
	}
	vec2 c = px + 0.5;
	float maxR = min(r.z, r.w) / 2.0;
	float R;
	vec2 o;
	if (c.x < r.x + r.z/2.0 && c.y < r.y + r.w/2.0) {
		R = min(rad.x, maxR);
		o = vec2(r.x + R, r.y + R);
		if (c.x >= o.x || c.y >= o.y) return cov;
	} else if (c.y < r.y + r.w/2.0) {
		R = min(rad.y, maxR);
		o = vec2(r.x + r.z - R, r.y + R);
		if (c.x <= o.x || c.y >= o.y) return cov;
	} else if (c.x < r.x + r.z/2.0) {
		R = min(rad.z, maxR);
		o = vec2(r.x + R, r.y + r.w - R);
		if (c.x >= o.x || c.y <= o.y) return cov;
	} else {
		R = min(rad.w, maxR);
		o = vec2(r.x + r.z - R, r.y + r.w - R);
		if (c.x <= o.x || c.y <= o.y) return cov;
	}
	return min(cov, clamp(0.5 - (length(c - o) - R), 0.0, 1.0));
}

// q8 converts a coverage in [0, 1] to [0, 255].
float q8(float cov) { return floor(clamp(cov, 0.0, 1.0)*255.0 + 0.5); }

// u8 converts a normalized channel to [0, 255].
float u8(float v) { return floor(v*255.0 + 0.5); }

// mul8 returns a*b/255 rounded, of values in [0, 255].
float mul8(float a, float b) { return floor((a*b + 127.0) / 255.0); }

// texel returns the texel of n/d in [0, size), truncating like the CPU painter. GPUs divide by
// multiplying by an approximate reciprocal, which misses exact quotients by an ulp: near an
// integer k the quotient is settled by comparing n with k*d.
int texel(float n, float d, int size) {
	float q = n / d;
	float k = floor(q + 0.5);
	if (abs(q - k) < 1e-3) {
		q = n >= k*d ? k : k - 1.0;
	}
	return clamp(int(q), 0, size - 1);
}

void main() {
	vec2 px = vec2(floor(gl_FragCoord.x), floor(uFB.y - gl_FragCoord.y));
	vec2 c = px + 0.5;
	vec3 rgb = vec3(u8(vColor.r), u8(vColor.g), u8(vColor.b));
	float a = u8(vColor.a);
	float cov;
	if (vKind == 0) { // Rectangle.
		cov = q8(rounded(px, vRect, vRadii));
	} else if (vKind == 1) { // Border: param holds the left, right, top and bottom widths.
		vec4 w = vParam;
		vec4 inner = vec4(vRect.x + w.x, vRect.y + w.z, vRect.z - w.x - w.y, vRect.w - w.w - w.z);
		float minW = min(min(w.x, w.y), min(w.z, w.w));
		float v = rounded(px, vRect, vRadii);
		if (inner.z > 0.0 && inner.w > 0.0) {
			v -= rounded(px, inner, max(vRadii - minW, 0.0));
		}
		cov = q8(v);
	} else if (vKind == 2) { // Circle inscribed in the rectangle.
		vec2 center = vRect.xy + vRect.zw/2.0;
		cov = q8(0.5 - (length(c - center) - min(vRect.z, vRect.w)/2.0));
	} else if (vKind == 3) { // Line: rect holds start and extent, param.x the half width.
		vec2 p = c - vRect.xy;
		vec2 d = vRect.zw;
		float l2 = dot(d, d);
		float t = l2 > 0.0 ? clamp(dot(p, d)/l2, 0.0, 1.0) : 0.0;
		cov = q8(0.5 - (length(p - t*d) - vParam.x));
	} else if (vKind == 4) { // Glyph: param holds the offset to atlas pixels.
		cov = u8(texelFetch(uAtlas, ivec2(px + vParam.xy), 0).r);
	} else { // Image, nearest sampled: param holds its size.
		ivec2 size = ivec2(vParam.xy);
		int sx = texel((c.x - vRect.x)*vParam.x, vRect.z, size.x);
		int sy = texel((c.y - vRect.y)*vParam.y, vRect.w, size.y);
		vec4 s = texelFetch(uImage, ivec2(sx, sy), 0);
		rgb = vec3(mul8(u8(s.r), rgb.r), mul8(u8(s.g), rgb.g), mul8(u8(s.b), rgb.b));
		a = mul8(u8(s.a), a);
		cov = q8(rounded(px, vRect, vRadii));
	}
	float alpha = mul8(a, cov) / 255.0;
	fragColor = vec4(rgb/255.0*alpha, alpha); // Premultiplied.
}
`
