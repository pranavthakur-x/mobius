package tui

// Viewport tracks bottom-anchored transcript scrolling.
type Viewport struct {
	Height       int
	ContentLines int
	Offset       int // lines from bottom; 0 = pinned
	AutoFollow   bool
	Unread       int
}

func NewViewport() Viewport {
	return Viewport{AutoFollow: true}
}

func (v *Viewport) SetContentLines(n int) {
	v.ContentLines = n
	v.clamp()
}

func (v *Viewport) clamp() {
	maxOff := v.maxOffset()
	if v.Offset > maxOff {
		v.Offset = maxOff
	}
	if v.Offset < 0 {
		v.Offset = 0
	}
}

func (v *Viewport) maxOffset() int {
	if v.Height <= 0 {
		return 0
	}
	d := v.ContentLines - v.Height
	if d < 0 {
		return 0
	}
	return d
}

func (v *Viewport) PinBottom() {
	v.Offset = 0
	v.AutoFollow = true
	v.Unread = 0
}

func (v *Viewport) ScrollUp(lines int) {
	if lines <= 0 {
		return
	}
	wasBottom := v.Offset == 0
	v.Offset += lines
	v.clamp()
	if wasBottom && v.Offset > 0 {
		v.AutoFollow = false
	}
}

func (v *Viewport) ScrollDown(lines int) {
	if lines <= 0 {
		return
	}
	v.Offset -= lines
	if v.Offset <= 0 {
		v.Offset = 0
		v.AutoFollow = true
		v.Unread = 0
	}
}

func (v *Viewport) PageUp()   { v.ScrollUp(v.Height) }
func (v *Viewport) PageDown() { v.ScrollDown(v.Height) }

func (v *Viewport) OnNewContent(linesAdded int) {
	if v.AutoFollow {
		v.Offset = 0
		v.Unread = 0
	} else {
		v.Unread += linesAdded
	}
}

// VisibleRange returns [start,end) line indices into content for the current window.
func (v *Viewport) VisibleRange() (start, end int) {
	if v.Height <= 0 {
		return 0, 0
	}
	end = v.ContentLines - v.Offset
	start = end - v.Height
	if start < 0 {
		start = 0
	}
	if end > v.ContentLines {
		end = v.ContentLines
	}
	return start, end
}

// ThumbPosition returns scrollbar thumb line index within track height.
func (v *Viewport) ThumbPosition(track int) int {
	if track <= 1 || v.ContentLines <= v.Height {
		return 0
	}
	maxOff := v.maxOffset()
	if maxOff == 0 {
		return 0
	}
	return (v.Offset * (track - 1)) / maxOff
}
