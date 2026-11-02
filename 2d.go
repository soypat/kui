package kui

// Scalar is the coordinate type used throughout kui.
type Scalar = float32

// Point is a 2D position.
type Point struct {
	X, Y Scalar
}

// Add returns p+q.
func (p Point) Add(q Point) Point { return Point{X: p.X + q.X, Y: p.Y + q.Y} }

// Sub returns p-q.
func (p Point) Sub(q Point) Point { return Point{X: p.X - q.X, Y: p.Y - q.Y} }

// Size is a width and height.
type Size struct {
	W, H Scalar
}

// Rect is an axis aligned rectangle with origin at its top left corner.
type Rect struct {
	X, Y, W, H Scalar
}

// Min returns the top left corner of r.
func (r Rect) Min() Point { return Point{X: r.X, Y: r.Y} }

// Max returns the bottom right corner of r.
func (r Rect) Max() Point { return Point{X: r.X + r.W, Y: r.Y + r.H} }

// Size returns the size of r.
func (r Rect) Size() Size { return Size{W: r.W, H: r.H} }

// Empty reports whether r has no area.
func (r Rect) Empty() bool { return !(r.W > 0 && r.H > 0) }

// Contains reports whether p lies within r, including its edges.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H
}

// Intersect returns the intersection of r and s. The result is empty if they do not overlap.
func (r Rect) Intersect(s Rect) Rect {
	x0, y0 := max(r.X, s.X), max(r.Y, s.Y)
	x1, y1 := min(r.X+r.W, s.X+s.W), min(r.Y+r.H, s.Y+s.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{X: x0, Y: y0}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Union returns the smallest rectangle containing r and s. Empty rectangles are ignored.
func (r Rect) Union(s Rect) Rect {
	if r.Empty() {
		return s
	} else if s.Empty() {
		return r
	}
	x0, y0 := min(r.X, s.X), min(r.Y, s.Y)
	x1, y1 := max(r.X+r.W, s.X+s.W), max(r.Y+r.H, s.Y+s.H)
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Inset returns r shrunk by in on each side.
func (r Rect) Inset(in Insets) Rect {
	return Rect{X: r.X + in.Left, Y: r.Y + in.Top, W: r.W - in.Left - in.Right, H: r.H - in.Top - in.Bottom}
}

// Insets are distances from each edge of a rectangle, i.e. padding.
type Insets struct {
	Left, Right, Top, Bottom Scalar
}

// InsetsAll returns insets of v on every side.
func InsetsAll(v Scalar) Insets { return Insets{Left: v, Right: v, Top: v, Bottom: v} }
