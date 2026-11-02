package internal

import "time"

// Monotonic taken from github.com/soypat/lneto/internal/monotonic.go

// MonotonicTime is a monotonic time source. Must be configured before use.
type MonotonicTime struct {
	src func() int64
	alt time.Time
}

// Config configures the clock with hal. If hal is nil will use [time.Since] in its stead.
func (m *MonotonicTime) Config(hal func() int64) {
	m.src = hal
	if hal == nil {
		m.alt = time.Now()
	}
}

// Nanotime returns the monotonic nanoseconds of configured hal. If hal=nil will return time since configuration with [time.Since].
func (m *MonotonicTime) Nanotime() (nanos int64) {
	if m.src != nil {
		nanos = m.src()
	} else {
		if m.alt.IsZero() {
			m.alt = time.Now()
		}
		nanos = time.Since(m.alt).Nanoseconds()
	}
	return nanos
}

// HAL returns the hal Monotonic was configured with.
func (m *MonotonicTime) HAL() func() int64 {
	return m.src
}
