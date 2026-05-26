//go:build windows

package winui

func (c *Controller) listViewHeight() int {
	return winH - listTop - listPad
}

func (c *Controller) contentHeight(n int) int {
	if n == 0 {
		return 0
	}
	return n * rowH
}

func (c *Controller) maxScrollPos(n int) int {
	d := c.contentHeight(n) - c.listViewHeight()
	if d < 0 {
		return 0
	}
	return d
}

func (c *Controller) clampScroll(n int) {
	max := c.maxScrollPos(n)
	if c.scrollY < 0 {
		c.scrollY = 0
	}
	if c.scrollY > max {
		c.scrollY = max
	}
}

func (c *Controller) scrollBy(delta int, rowCount int) {
	c.scrollY += delta
	c.clampScroll(rowCount)
}

func (c *Controller) scrollRect() rect {
	return rect{
		Left:   scrollX,
		Top:    int32(listTop),
		Right:  scrollX + scrollBarW,
		Bottom: int32(winH - listPad),
	}
}

func (c *Controller) thumbRect(rowCount int) rect {
	sr := c.scrollRect()
	trackH := int(sr.Bottom - sr.Top)
	if trackH <= 0 || rowCount == 0 {
		return sr
	}
	content := c.contentHeight(rowCount)
	viewH := c.listViewHeight()
	if content <= viewH {
		return rect{sr.Left + 2, sr.Top + 2, sr.Right - 2, sr.Bottom - 2}
	}
	thumbH := trackH * viewH / content
	if thumbH < 28 {
		thumbH = 28
	}
	maxScroll := c.maxScrollPos(rowCount)
	frac := 0.0
	if maxScroll > 0 {
		frac = float64(c.scrollY) / float64(maxScroll)
	}
	top := int(sr.Top) + int(float64(trackH-thumbH)*frac)
	return rect{
		Left:   sr.Left + 2,
		Top:    int32(top),
		Right:  sr.Right - 2,
		Bottom: int32(top + thumbH),
	}
}

func (c *Controller) scrollFromThumbY(y int, rowCount int) {
	sr := c.scrollRect()
	trackH := int(sr.Bottom - sr.Top)
	thumb := c.thumbRect(rowCount)
	thumbH := int(thumb.Bottom - thumb.Top)
	maxScroll := c.maxScrollPos(rowCount)
	if maxScroll <= 0 || trackH <= thumbH {
		c.scrollY = 0
		return
	}
	rel := y - int(sr.Top) - thumbH/2
	if rel < 0 {
		rel = 0
	}
	if rel > trackH-thumbH {
		rel = trackH - thumbH
	}
	frac := float64(rel) / float64(trackH-thumbH)
	c.scrollY = int(frac * float64(maxScroll))
	c.clampScroll(rowCount)
}
