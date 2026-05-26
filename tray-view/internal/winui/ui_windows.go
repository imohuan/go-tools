//go:build windows

package winui

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"tray-view/internal/process"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procFillRect         = user32.NewProc("FillRect")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procDrawIconEx       = user32.NewProc("DrawIconEx")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procReleaseCapture   = user32.NewProc("ReleaseCapture")
	procSetCapture       = user32.NewProc("SetCapture")
	procSetFocus         = user32.NewProc("SetFocus")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procMoveToEx         = gdi32.NewProc("MoveToEx")
	procLineTo           = gdi32.NewProc("LineTo")
	procCreatePen        = gdi32.NewProc("CreatePen")
	procTrackMouseEvent = user32.NewProc("TrackMouseEvent")
	procMessageBoxW     = user32.NewProc("MessageBoxW")
	procGetClientRect   = user32.NewProc("GetClientRect")
	procRedrawWindow    = user32.NewProc("RedrawWindow")
)

type trackMouseEvent struct {
	Size        uint32
	Flags       uint32
	Hwnd        syscall.Handle
	DwHoverTime uint32
}

const (
	winClass = "TrayViewPopup"
	tmeLeave = 0x00000002

	wsExTopmost = 0x00000008
	wsPopup     = 0x80000000
	wsBorder    = 0x00800000
	swShow      = 5
	swHide      = 0
	swPShow     = 0x0040

	wmDestroy        = 0x0002
	wmPaint          = 0x000F
	wmClose          = 0x0010
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmMouseMove      = 0x0200
	wmMouseWheel     = 0x020A
	wmMouseLeave     = 0x02A3
	wmNcHitTest      = 0x0084
	wmCaptureChanged = 0x0215

	dtLeft        = 0x00000000
	dtVCenter     = 0x00000004
	dtSingleLine  = 0x00000020
	dtEndEllipsis = 0x00008000
	transparent   = 1

	htClient  = 1
	htCaption = 2
	refreshMS = 4000
)

const (
	hitNone = iota
	hitCaption
	hitBtnMin
	hitBtnClose
	hitSortName
	hitSortMem
	hitSortCPU
	hitCheck
	hitRow
	hitScrollThumb
	hitScrollTrack
)

type wndClassEx struct {
	Size, Style       uint32
	WndProc           uintptr
	ClsExtra, WndExtra int32
	Instance          syscall.Handle
	Icon, Cursor      syscall.Handle
	Background        syscall.Handle
	MenuName          *uint16
	ClassName         *uint16
	IconSm            syscall.Handle
}

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	HDC       syscall.Handle
	Erase     int32
	_pad0     [4]byte
	RcPaint   rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type msg struct {
	Hwnd                         syscall.Handle
	Message                      uint32
	WParam, LParam               uintptr
	Time                         uint32
	Pt                           point
}

// Controller owns the popup window.
type Controller struct {
	hwnd    syscall.Handle
	visible bool
	mu      sync.Mutex

	snap    *process.Snapshot
	rows    []uiRow
	checked map[string]bool

	sortBy  SortField
	sortAsc bool

	hover      int
	hoverBtn   int
	scrollY    int
	scrollHot  bool
	scrollDrag bool

	dragging   bool
	trackLeave bool

	font, fontTitle, fontHeader, fontMetrics syscall.Handle
	brushBG, brushTitle, brushHeader, brushListBg                    syscall.Handle
	brushRowEven, brushRowOdd, brushRowHover                        syscall.Handle
	brushBtn, brushBtnHover, brushChkOff, brushChkOn                syscall.Handle
	brushBarTrack, brushBarGreen, brushBarYellow, brushBarRed       syscall.Handle
	brushScrollTrack, brushScrollThumb, brushScrollThumbHot         syscall.Handle
	brushBorder, brushCloseHover                                    syscall.Handle

	ready     chan struct{}
	toggleCh  chan struct{}
}

// New creates the controller.
func New() *Controller {
	return &Controller{
		hover:    -1,
		hoverBtn: -1,
		checked:  map[string]bool{"trash": true},
		sortBy:   SortByMem,
		sortAsc:  false,
		ready:    make(chan struct{}),
		toggleCh: make(chan struct{}, 4),
	}
}

// SelectedIDs returns checked row IDs.
func (c *Controller) SelectedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for id, on := range c.checked {
		if on {
			out = append(out, id)
		}
	}
	return out
}

// Run starts the message loop.
func (c *Controller) Run() {
	hInst := getModuleHandle()
	className, _ := windows.UTF16PtrFromString(winClass)
	wc := wndClassEx{
		Size: uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc: syscall.NewCallback(c.wndProc), Instance: hInst, ClassName: className,
		Background: solidBrush(0x121212),
	}
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	wc.Cursor = syscall.Handle(cursor)
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := windows.UTF16PtrFromString("进程查看")
	c.hwnd = syscall.Handle(createWindow(
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup|wsBorder), 80, 80, winW, winH, 0, 0, uintptr(hInst), 0,
	))
	if c.hwnd == 0 || !isWindow(c.hwnd) {
		txt, _ := windows.UTF16PtrFromString("无法创建进程查看窗口。")
		cap, _ := windows.UTF16PtrFromString("tray-view")
		procMessageBoxW.Call(0, uintptr(unsafe.Pointer(txt)), uintptr(unsafe.Pointer(cap)), 0x10)
		close(c.ready)
		return
	}

	c.initGDI()
	close(c.ready)
	go c.refreshWorker()
	c.loadSnapshotAsync()
	procShowWindow.Call(uintptr(c.hwnd), swHide)

	c.messagePump()
	c.freeGDI()
}

func (c *Controller) refreshWorker() {
	tick := time.NewTicker(refreshMS * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		if !c.IsVisible() {
			continue
		}
		snap, err := process.Collect()
		if err != nil {
			continue
		}
		c.mu.Lock()
		c.snap = snap
		c.rows = buildRows(snap)
		c.clampScroll(len(c.rows))
		c.mu.Unlock()
		procPostMessageW.Call(uintptr(c.hwnd), wmUserRefresh, 0, 0)
	}
}

func (c *Controller) loadSnapshotAsync() {
	go func() {
		snap, err := process.Collect()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.snap = snap
		c.rows = buildRows(snap)
		c.clampScroll(len(c.rows))
		c.mu.Unlock()
		if c.hwnd != 0 {
			procPostMessageW.Call(uintptr(c.hwnd), wmUserRefresh, 0, 0)
		}
	}()
}

func (c *Controller) wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmClose:
		c.hide()
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	case wmEraseBkgnd:
		var rc rect
		procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
		procFillRect.Call(wParam, uintptr(unsafe.Pointer(&rc)), uintptr(c.brushBG))
		return 1
	case wmUserRefresh:
		c.invalidateListAndScroll(hwnd)
		return 0
	case wmPaint:
		c.onPaint(hwnd)
		return 0
	case wmNcHitTest:
		return c.onNcHitTest(hwnd, lParam)
	case wmLButtonDown:
		c.onLButtonDown(hwnd, lParam)
		return 0
	case wmLButtonUp:
		c.onLButtonUp(hwnd, lParam)
		return 0
	case wmMouseMove:
		c.onMouseMove(hwnd, lParam)
		return 0
	case wmMouseLeave:
		c.trackLeave = false
		c.mu.Lock()
		oldHover := c.hover
		oldBtn := c.hoverBtn
		scroll := c.scrollY
		scrollHot := c.scrollHot
		c.hover, c.hoverBtn = -1, -1
		c.scrollHot = false
		c.mu.Unlock()
		if oldHover >= 0 {
			c.invalidateRow(hwnd, oldHover, scroll)
		}
		if oldBtn != -1 {
			invalidateRect(hwnd, c.titleBarRect())
		}
		if scrollHot {
			invalidateRect(hwnd, c.scrollBarRect())
		}
		return 0
	case wmMouseWheel:
		c.onMouseWheel(hwnd, wParam)
		return 0
	case wmCaptureChanged:
		c.scrollDrag = false
		c.dragging = false
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func (c *Controller) onMouseWheel(hwnd syscall.Handle, wParam uintptr) {
	delta := int16((wParam >> 16) & 0xffff)
	step := -int(delta) / 120 * rowH
	if step == 0 {
		if delta > 0 {
			step = -rowH
		} else {
			step = rowH
		}
	}
	c.mu.Lock()
	before := c.scrollY
	c.scrollBy(step, len(c.rows))
	changed := before != c.scrollY
	c.mu.Unlock()
	if changed {
		c.invalidateListAndScroll(hwnd)
	}
}

func (c *Controller) onNcHitTest(hwnd syscall.Handle, lParam uintptr) uintptr {
	sx := int16(lParam & 0xffff)
	sy := int16((lParam >> 16) & 0xffff)
	pt := point{X: int32(sx), Y: int32(sy)}
	procScreenToClient.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	hit, _ := c.hitTest(int(pt.X), int(pt.Y))
	if hit == hitCaption {
		return htCaption
	}
	return htClient
}

func (c *Controller) onLButtonDown(hwnd syscall.Handle, lParam uintptr) {
	x, y := clientXY(lParam)
	hit, row := c.hitTest(x, y)
	c.mu.Lock()
	n := len(c.rows)
	c.mu.Unlock()

	switch hit {
	case hitBtnClose, hitBtnMin:
		c.hide()
	case hitCaption:
		c.dragging = true
		procSetCapture.Call(uintptr(hwnd))
	case hitSortName:
		c.toggleSort(SortByName)
		c.invalidateListAndScroll(hwnd)
	case hitSortMem:
		c.toggleSort(SortByMem)
		c.invalidateListAndScroll(hwnd)
	case hitSortCPU:
		c.toggleSort(SortByCPU)
		c.invalidateListAndScroll(hwnd)
	case hitScrollThumb, hitScrollTrack:
		c.scrollDrag = true
		procSetCapture.Call(uintptr(hwnd))
		c.mu.Lock()
		if hit == hitScrollTrack {
			c.scrollFromThumbY(y, n)
		}
		c.mu.Unlock()
		c.invalidateListAndScroll(hwnd)
	case hitRow:
		c.mu.Lock()
		oldHover := c.hover
		scroll := c.scrollY
		c.hover = row
		c.mu.Unlock()
		if oldHover != row {
			c.invalidateRow(hwnd, oldHover, scroll)
			c.invalidateRow(hwnd, row, scroll)
		}
	}
}

func (c *Controller) onLButtonUp(hwnd syscall.Handle, lParam uintptr) {
	x, y := clientXY(lParam)
	hit, row := c.hitTest(x, y)
	if hit == hitCheck {
		c.mu.Lock()
		scroll := c.scrollY
		c.mu.Unlock()
		c.toggleCheck(row)
		c.invalidateRow(hwnd, row, scroll)
	}
	if c.dragging || c.scrollDrag {
		c.dragging, c.scrollDrag = false, false
		procReleaseCapture.Call()
	}
}

func (c *Controller) onMouseMove(hwnd syscall.Handle, lParam uintptr) {
	x, y := clientXY(lParam)
	if y >= listTop && !c.trackLeave {
		c.trackLeave = true
		tme := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: tmeLeave, Hwnd: hwnd}
		procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
	}

	hit, row := c.hitTest(x, y)
	hoverRow := row
	if hit != hitRow && hit != hitCheck {
		hoverRow = -1
	}

	btn := -1
	if hit == hitBtnClose {
		btn = 1
	} else if hit == hitBtnMin {
		btn = 0
	}

	scrollHot := hit == hitScrollThumb || hit == hitScrollTrack

	c.mu.Lock()
	oldHover := c.hover
	oldBtn := c.hoverBtn
	oldScrollHot := c.scrollHot
	scroll := c.scrollY
	n := len(c.rows)

	hoverChanged := oldHover != hoverRow
	btnChanged := oldBtn != btn
	scrollHotChanged := oldScrollHot != scrollHot

	c.hover, c.hoverBtn, c.scrollHot = hoverRow, btn, scrollHot
	scrollPosChanged := false
	if c.scrollDrag {
		before := c.scrollY
		c.scrollFromThumbY(y, n)
		scrollPosChanged = before != c.scrollY
		scroll = c.scrollY
	}
	c.mu.Unlock()

	if btnChanged {
		invalidateRect(hwnd, c.titleBarRect())
	}
	if scrollHotChanged || scrollPosChanged {
		invalidateRect(hwnd, c.scrollBarRect())
	}
	if scrollPosChanged {
		invalidateRect(hwnd, c.listContentRect())
	}
	if hoverChanged {
		c.invalidateRow(hwnd, oldHover, scroll)
		c.invalidateRow(hwnd, hoverRow, scroll)
	}
}

func (c *Controller) toggleCheck(displayIdx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows := sortRows(c.rows, c.sortBy, c.sortAsc)
	if displayIdx < 0 || displayIdx >= len(rows) {
		return
	}
	id := rows[displayIdx].ID
	c.checked[id] = !c.checked[id]
}

func (c *Controller) hitTest(x, y int) (int, int) {
	if y < titleBarH {
		if x >= winW-44 {
			return hitBtnClose, -1
		}
		if x >= winW-88 && x < winW-48 {
			return hitBtnMin, -1
		}
		if x < winW-92 {
			return hitCaption, -1
		}
		return hitNone, -1
	}

	if y >= titleBarH && y < listTop {
		if x >= 52 && x < 150 {
			return hitSortName, -1
		}
		if x >= 200 && x < 280 {
			return hitSortMem, -1
		}
		if x >= 290 && x < listRight {
			return hitSortCPU, -1
		}
		return hitNone, -1
	}

	sr := c.scrollRect()
	if x >= int(sr.Left) && x <= int(sr.Right) && y >= int(sr.Top) && y <= int(sr.Bottom) {
		c.mu.Lock()
		n := len(c.rows)
		c.mu.Unlock()
		thumb := c.thumbRect(n)
		if x >= int(thumb.Left) && x <= int(thumb.Right) && y >= int(thumb.Top) && y <= int(thumb.Bottom) {
			return hitScrollThumb, -1
		}
		return hitScrollTrack, -1
	}

	c.mu.Lock()
	by, asc := c.sortBy, c.sortAsc
	raw := c.rows
	scroll := c.scrollY
	c.mu.Unlock()
	rows := sortRows(raw, by, asc)
	n := len(rows)

	ly := y - listTop + scroll
	if ly < 0 {
		return hitNone, -1
	}
	row := ly / rowH
	if row < 0 || row >= n {
		return hitNone, -1
	}
	ry := listTop + row*rowH - scroll
	chk := rect{listLeft + 6, int32(ry + 14), listLeft + 22, int32(ry + 28)}
	if x >= int(chk.Left) && x <= int(chk.Right) && y >= int(chk.Top) && y <= int(chk.Bottom) {
		return hitCheck, row
	}
	if x >= listLeft && x < listRight {
		return hitRow, row
	}
	return hitNone, -1
}

func clientXY(lParam uintptr) (int, int) {
	return int(int32(lParam & 0xffff)), int(int32((lParam >> 16) & 0xffff))
}

func (c *Controller) onPaint(hwnd syscall.Handle) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	c.paintFrame(hdc)
}

func (c *Controller) clientSize() (int, int) {
	var rc rect
	procGetClientRect.Call(uintptr(c.hwnd), uintptr(unsafe.Pointer(&rc)))
	return int(rc.Right - rc.Left), int(rc.Bottom - rc.Top)
}

func (c *Controller) paintFrame(hdc uintptr) {
	cw, ch := c.clientSize()
	if cw <= 0 {
		cw = winW
	}
	if ch <= 0 {
		ch = winH
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{0, 0, int32(cw), int32(ch)})), uintptr(c.brushBG))

	c.mu.Lock()
	raw := c.rows
	sortBy := c.sortBy
	sortAsc := c.sortAsc
	checked := make(map[string]bool, len(c.checked))
	for k, v := range c.checked {
		checked[k] = v
	}
	hover := c.hover
	hoverBtn := c.hoverBtn
	scroll := c.scrollY
	scrollHot := c.scrollHot
	c.mu.Unlock()

	rows := sortRows(raw, sortBy, sortAsc)

	c.drawTitleBar(hdc, hoverBtn)
	c.drawSortHeader(hdc, sortBy, sortAsc)

	listArea := c.listContentRect()
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&listArea)), uintptr(c.brushListBg))
	procFrameRect(hdc, listArea, c.brushBorder)

	if len(rows) == 0 {
		drawText(hdc, c.font, 0x888888, listLeft, listTop+20, listW, rowH, "正在加载进程列表…")
	} else {
		barW := 100
		if listW-200 > barW {
			barW = listW - 200
		}
		for i, row := range rows {
			ry := listTop + i*rowH - scroll
			if ry+rowH < listTop || ry > winH-listPad {
				continue
			}
			on := checked[row.ID]
			c.drawRow(hdc, ry, i, row, on, hover == i, barW)
		}
	}

	c.drawScrollbar(hdc, len(rows), scrollHot)
}

func rectsIntersect(a, b rect) bool {
	return a.Left < b.Right && a.Right > b.Left && a.Top < b.Bottom && a.Bottom > b.Top
}

func procFrameRect(hdc uintptr, r rect, brush syscall.Handle) {
	// top + bottom + sides using fill rects
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{r.Left, r.Top, r.Right, r.Top + 1})), uintptr(brush))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{r.Left, r.Bottom - 1, r.Right, r.Bottom})), uintptr(brush))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{r.Left, r.Top, r.Left + 1, r.Bottom})), uintptr(brush))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{r.Right - 1, r.Top, r.Right, r.Bottom})), uintptr(brush))
}

func (c *Controller) drawTitleBar(hdc uintptr, hoverBtn int) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{0, 0, winW, titleBarH})), uintptr(c.brushTitle))
	drawText(hdc, c.fontTitle, 0xf0f0f0, 16, 12, 180, titleBarH, "进程查看")

	minR := rect{winW - 88, 8, winW - 48, titleBarH - 8}
	closeR := rect{winW - 44, 8, winW - 8, titleBarH - 8}
	c.drawTitleButton(hdc, minR, hoverBtn == 0, false)
	c.drawTitleButton(hdc, closeR, hoverBtn == 1, true)
}

func (c *Controller) drawTitleButton(hdc uintptr, r rect, hot, close bool) {
	br := c.brushBtn
	if hot {
		br = c.brushBtnHover
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), uintptr(br))
	cx := (r.Left + r.Right) / 2
	cy := (r.Top + r.Bottom) / 2
	col := uint32(0xbbbbbb)
	if hot {
		col = 0xffffff
	}
	if close && hot {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), uintptr(c.brushCloseHover))
		col = 0xffffff
	}
	if close {
		drawText(hdc, c.fontTitle, col, int(cx-4), int(cy-7), 12, 18, "×")
	} else {
		drawText(hdc, c.fontTitle, col, int(cx-6), int(cy-2), 14, 12, "―")
	}
}

func (c *Controller) drawSortHeader(hdc uintptr, active SortField, asc bool) {
	h := rect{0, int32(titleBarH), winW, int32(listTop)}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&h)), uintptr(c.brushHeader))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{listLeft, int32(titleBarH + headerH - 1), listRight, int32(listTop)})), uintptr(c.brushBorder))

	drawSortCol := func(x int, label string, field SortField) {
		col := uint32(0x777777)
		if field == active {
			col = 0x5cadff
		}
		drawText(hdc, c.fontHeader, col, x, titleBarH+8, 90, headerH, sortLabel(field, active, asc))
	}
	drawSortCol(52, "名称", SortByName)
	drawSortCol(200, "内存", SortByMem)
	drawSortCol(300, "CPU", SortByCPU)
}

func (c *Controller) drawScrollbar(hdc uintptr, rowCount int, hot bool) {
	sr := c.scrollRect()
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&sr)), uintptr(c.brushScrollTrack))
	if rowCount == 0 || c.contentHeight(rowCount) <= c.listViewHeight() {
		return
	}
	thumb := c.thumbRect(rowCount)
	br := c.brushScrollThumb
	if hot || c.scrollDrag {
		br = c.brushScrollThumbHot
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&thumb)), uintptr(br))
}

func (c *Controller) drawRow(hdc uintptr, y, index int, row uiRow, checked, hover bool, barW int) {
	rowR := rect{listLeft, int32(y + 1), listRight, int32(y + rowH - 1)}
	bg := c.brushRowEven
	if index%2 == 1 {
		bg = c.brushRowOdd
	}
	if hover {
		bg = c.brushRowHover
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rowR)), uintptr(bg))

	x := listLeft + 8
	c.drawCheckbox(hdc, x, y+14, checked)

	x += 28
	c.drawAppIcon(hdc, x, y+11, row)

	x += 30
	labelW := listRight - x - 16
	if hover {
		labelW = 130
	}
	drawText(hdc, c.font, 0xececec, x, y+13, int(labelW), 22, row.Label)

	if hover {
		metrics := fmt.Sprintf("内存 %.1f MB   CPU %.2f%%", row.MemMB, row.CPUPercent)
		drawText(hdc, c.fontMetrics, 0xff9e6d, listRight-210, y+14, 200, 22, metrics)
	} else {
		barX := listRight - barW - 8
		c.drawBar(hdc, int(barX), y+18, barW, 8, row.MemMB, row.CPUPercent, row.SystemBar)
	}
}

func (c *Controller) drawCheckbox(hdc uintptr, x, y int, on bool) {
	outer := rect{int32(x), int32(y), int32(x + 16), int32(y + 16)}
	if on {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&outer)), uintptr(c.brushChkOn))
		drawCheckMark(hdc, x+3, y+3)
	} else {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&outer)), uintptr(c.brushChkOff))
		procFrameRect(hdc, outer, c.brushBorder)
	}
}

func drawCheckMark(hdc uintptr, x, y int) {
	pen, _, _ := procCreatePen.Call(0, 2, 0xffffff)
	old, _, _ := procSelectObject.Call(hdc, pen)
	procMoveToEx.Call(hdc, uintptr(x), uintptr(y+6), 0)
	procLineTo.Call(hdc, uintptr(x+4), uintptr(y+10))
	procLineTo.Call(hdc, uintptr(x+11), uintptr(y+2))
	procSelectObject.Call(hdc, old)
	procDeleteObject.Call(pen)
}

func (c *Controller) drawAppIcon(hdc uintptr, x, y int, row uiRow) {
	const size = 24
	if h := c.iconForRow(row); h != 0 {
		procDrawIconEx.Call(hdc, uintptr(x), uintptr(y), uintptr(h), size, size, 0, 0, 0x0003)
		return
	}
	icon := rect{int32(x), int32(y), int32(x + size), int32(y + size)}
	brush := solidBrush(iconColor(row.Label))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&icon)), uintptr(brush))
	procDeleteObject.Call(uintptr(brush))
}

func (c *Controller) iconForRow(row uiRow) syscall.Handle {
	switch row.ID {
	case "trash":
		if h := process.SmallIcon(`C:\Windows\System32\cleanmgr.exe`); h != 0 {
			return h
		}
		return process.SmallIcon(`C:\Windows\explorer.exe`)
	case "__system__":
		return process.SmallIcon(`C:\Windows\explorer.exe`)
	}
	if row.ExePath != "" {
		return process.SmallIcon(row.ExePath)
	}
	return 0
}

func (c *Controller) drawBar(hdc uintptr, x, y, w, h int, mem, cpu float64, system bool) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{int32(x), int32(y), int32(x + w), int32(y + h)})), uintptr(c.brushBarTrack))
	usage := mem / 2048
	if cpu > 0 {
		usage = usage*0.6 + (cpu/100)*0.4
	}
	if system {
		usage = 0.82
	}
	if usage < 0.04 {
		usage = 0.04
	}
	if usage > 1 {
		usage = 1
	}
	fw := int(float64(w) * usage)
	if fw < 2 {
		fw = 2
	}
	if system {
		seg := fw / 3
		if seg < 1 {
			seg = 1
		}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{int32(x), int32(y), int32(x + seg), int32(y + h)})), uintptr(c.brushBarGreen))
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{int32(x + seg), int32(y), int32(x + seg * 2), int32(y + h)})), uintptr(c.brushBarYellow))
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{int32(x + seg*2), int32(y), int32(x + fw), int32(y + h)})), uintptr(c.brushBarRed))
	} else {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect{int32(x), int32(y), int32(x + fw), int32(y + h)})), uintptr(c.brushBarGreen))
	}
}

func drawText(hdc uintptr, font syscall.Handle, color uint32, x, y, w, h int, text string) {
	if font != 0 {
		procSelectObject.Call(hdc, uintptr(font))
	}
	procSetBkMode.Call(hdc, transparent)
	procSetTextColor.Call(hdc, uintptr(color))
	r := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
	p, _ := windows.UTF16PtrFromString(text)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(p)), ^uintptr(0),
		uintptr(unsafe.Pointer(&r)), dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis)
}

func iconColor(label string) uint32 {
	h := uint32(0)
	for _, ch := range label {
		h = h*31 + uint32(ch)
	}
	palette := []uint32{0x5c6bc0, 0x43a047, 0xe53935, 0xfb8c00, 0x8e24aa, 0x1e88e5}
	return palette[h%uint32(len(palette))]
}

func (c *Controller) toggleAt() {
	if c.windowActuallyVisible() {
		c.hide()
		return
	}
	c.ShowCenter()
}

func (c *Controller) ShowCenter() {
	if c.hwnd == 0 || !isWindow(c.hwnd) {
		return
	}
	x, y := WorkAreaCenter()
	c.visible = true
	c.loadSnapshotAsync()
	c.activateWindow(x, y)
	invalidateFull(c.hwnd)
	const rdwInvalidate = 0x0001
	const rdwErase = 0x0004
	const rdwUpdate = 0x0100
	procRedrawWindow.Call(uintptr(c.hwnd), 0, 0, rdwInvalidate|rdwErase|rdwUpdate)
}

func (c *Controller) Show(x, y int) {
	c.ShowCenter()
}

func (c *Controller) hide() {
	if c.hwnd == 0 {
		return
	}
	c.visible = false
	procShowWindow.Call(uintptr(c.hwnd), swHide)
}

func (c *Controller) Hide() { c.hide() }

func (c *Controller) IsVisible() bool {
	return c.visible && c.windowActuallyVisible()
}

func (c *Controller) Toggle() {
	c.toggleAt()
}

const WinW = winW
const WinH = winH

func getModuleHandle() syscall.Handle {
	h, _, _ := procGetModuleHandleW.Call(0)
	return syscall.Handle(h)
}

func createWindow(class, title, style uintptr, x, y, w, h int, parent, menu, inst, param uintptr) uintptr {
	r, _, _ := procCreateWindowExW.Call(wsExTopmost, class, title, style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, menu, inst, param)
	return r
}

func solidBrush(rgb uint32) syscall.Handle {
	r := byte(rgb & 0xff)
	g := byte((rgb >> 8) & 0xff)
	b := byte((rgb >> 16) & 0xff)
	col := uint32(b)<<16 | uint32(g)<<8 | uint32(r)
	h, _, _ := procCreateSolidBrush.Call(uintptr(col))
	return syscall.Handle(h)
}
