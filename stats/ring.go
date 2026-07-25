package stats

// ring is a FIFO queue of measurements backed by a single slice.
//
// The sliding window only ever appends at one end and expires from the
// other, which is exactly what a ring buffer does well. Storing measurements
// by value means a steady-state window allocates nothing: once the buffer has
// grown to the working set, entries are overwritten in place as the window
// slides, where a linked list would allocate a node per push and leave it for
// the collector on every expiry.
//
// The capacity is always a power of two so that wrapping an index is a mask
// rather than a division.
type ring struct {
	buf  []measurement
	head int // index of the oldest element
	n    int // number of elements held
}

const minRingCap = 8

func (r *ring) len() int { return r.n }

// at returns the i-th oldest element. It is only valid for i < r.n.
func (r *ring) at(i int) measurement {
	return r.buf[(r.head+i)&(len(r.buf)-1)]
}

// front returns the oldest element, reporting false if the ring is empty.
func (r *ring) front() (measurement, bool) {
	if r.n == 0 {
		return measurement{}, false
	}
	return r.buf[r.head], true
}

func (r *ring) push(m measurement) {
	if r.n == len(r.buf) {
		r.grow()
	}
	r.buf[(r.head+r.n)&(len(r.buf)-1)] = m
	r.n++
}

// pop discards the oldest element. It zeroes the vacated slot so the
// time.Time it held does not keep its location pointer alive.
func (r *ring) pop() {
	if r.n == 0 {
		return
	}
	r.buf[r.head] = measurement{}
	r.head = (r.head + 1) & (len(r.buf) - 1)
	r.n--
}

func (r *ring) reset() {
	clear(r.buf)
	r.head = 0
	r.n = 0
}

// grow doubles the capacity and unwraps the contents so the oldest element
// sits at index 0.
func (r *ring) grow() {
	capacity := 2 * len(r.buf)
	if capacity < minRingCap {
		capacity = minRingCap
	}
	buf := make([]measurement, capacity)
	if r.n > 0 {
		copied := copy(buf, r.buf[r.head:])
		copy(buf[copied:], r.buf[:r.head])
	}
	r.buf = buf
	r.head = 0
}
