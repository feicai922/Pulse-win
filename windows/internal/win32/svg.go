package win32

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"pulse-windows/assets"
)

type svgDoc struct {
	viewX, viewY, viewW, viewH float64
	isStroke                   bool
	strokeWidth                float64
	fillEvenOdd                bool
	compiledPaths              []uintptr
}

var (
	svgMu    sync.RWMutex
	svgCache = map[string]*svgDoc{}

	reViewBox     = regexp.MustCompile(`viewBox="([^"]+)"`)
	rePathTag     = regexp.MustCompile(`<path[^>]*>`)
	reDAttr       = regexp.MustCompile(`\bd="([^"]+)"`)
	reStrokeWidth = regexp.MustCompile(`stroke-width="([^"]+)"`)
)

func loadSVG(name string) *svgDoc {
	svgMu.RLock()
	if d, ok := svgCache[name]; ok {
		svgMu.RUnlock()
		return d
	}
	svgMu.RUnlock()

	var raw []byte
	var err error
	if name == "pulse-mark" {
		raw, err = assets.PulseMarkSVG()
	} else {
		raw, err = assets.IconSVG(name)
	}
	if err != nil {
		return nil
	}
	src := string(raw)
	fillEvenOdd := strings.Contains(src, `fill-rule="evenodd"`)
	doc := &svgDoc{
		viewW:       24,
		viewH:       24,
		fillEvenOdd: fillEvenOdd,
		isStroke:    strings.Contains(src, `fill="none"`),
		strokeWidth: 2.0,
	}
	if m := reViewBox.FindStringSubmatch(src); len(m) == 2 {
		parts := strings.Fields(m[1])
		if len(parts) == 4 {
			doc.viewX, _ = strconv.ParseFloat(parts[0], 64)
			doc.viewY, _ = strconv.ParseFloat(parts[1], 64)
			doc.viewW, _ = strconv.ParseFloat(parts[2], 64)
			doc.viewH, _ = strconv.ParseFloat(parts[3], 64)
		}
	}
	if m := reStrokeWidth.FindStringSubmatch(src); len(m) == 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
			doc.strokeWidth = v
		}
	}
	fillMode := int32(1)
	if fillEvenOdd {
		fillMode = 0
	}
	for _, tag := range rePathTag.FindAllString(src, -1) {
		if m := reDAttr.FindStringSubmatch(tag); len(m) == 2 {
			if p := buildSVGPath(m[1], fillMode); p != 0 {
				doc.compiledPaths = append(doc.compiledPaths, p)
			}
		}
	}

	svgMu.Lock()
	if existing, ok := svgCache[name]; ok {
		svgMu.Unlock()
		for _, p := range doc.compiledPaths {
			DeletePath(p)
		}
		return existing
	}
	svgCache[name] = doc
	svgMu.Unlock()
	return doc
}

// DrawSVGIcon renders a pre-compiled bundled SVG icon centered at (x, y, size, size).
func (c *LayeredCanvas) DrawSVGIcon(name string, x, y, size float32, argb uint32) bool {
	doc := loadSVG(name)
	if doc == nil || len(doc.compiledPaths) == 0 || doc.viewW <= 0 || doc.viewH <= 0 {
		return false
	}

	scale := float64(size) / math.Max(doc.viewW, doc.viewH)
	offX := float64(x) + (float64(size)-doc.viewW*scale)/2 - doc.viewX*scale
	offY := float64(y) + (float64(size)-doc.viewH*scale)/2 - doc.viewY*scale

	var mat uintptr
	_, _, _ = procGdipCreateMatrix.Call(uintptr(unsafe.Pointer(&mat)))
	if mat != 0 {
		_, _, _ = procGdipTranslateMatrix.Call(mat, f32(float32(offX)), f32(float32(offY)), 0)
		_, _, _ = procGdipScaleMatrix.Call(mat, f32(float32(scale)), f32(float32(scale)), 0)
	}
	defer func() {
		if mat != 0 {
			_, _, _ = procGdipDeleteMatrix.Call(mat)
		}
	}()

	for _, srcPath := range doc.compiledPaths {
		var path uintptr
		_, _, _ = procGdipClonePath.Call(srcPath, uintptr(unsafe.Pointer(&path)))
		if path == 0 {
			continue
		}
		if mat != 0 {
			_, _, _ = procGdipTransformPath.Call(path, mat)
		}
		if doc.isStroke {
			sw := float32(doc.strokeWidth * scale)
			if sw < 1.2 {
				sw = 1.2
			}
			c.DrawPath(path, sw, argb, true)
		} else {
			c.FillPath(path, argb)
		}
		DeletePath(path)
	}
	return true
}

func buildSVGPath(d string, fillMode int32) uintptr {
	tokens := tokenizeSVGPath(d)
	if len(tokens) == 0 {
		return 0
	}
	var path uintptr
	_, _, _ = procGdipCreatePath.Call(uintptr(fillMode), uintptr(unsafe.Pointer(&path)))
	if path == 0 {
		return 0
	}

	var curX, curY float64
	var startX, startY float64
	var lastCtrlX, lastCtrlY float64
	var lastCmd byte

	i := 0
	nextNum := func() (float64, bool) {
		if i >= len(tokens) {
			return 0, false
		}
		t := tokens[i]
		if len(t) == 1 && isSVGCmd(t[0]) {
			return 0, false
		}
		v, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, false
		}
		i++
		return v, true
	}
	nextFlag := func() (bool, bool) {
		v, ok := nextNum()
		return v != 0, ok
	}

	for i < len(tokens) {
		tok := tokens[i]
		var cmd byte
		if len(tok) == 1 && isSVGCmd(tok[0]) {
			cmd = tok[0]
			i++
		} else if lastCmd != 0 {
			cmd = lastCmd
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}
		} else {
			break
		}

		switch cmd {
		case 'M', 'm':
			first := true
			for {
				x, ok1 := nextNum()
				y, ok2 := nextNum()
				if !ok1 || !ok2 {
					break
				}
				if cmd == 'm' {
					x += curX
					y += curY
				}
				if first {
					_, _, _ = procGdipStartPathFigure.Call(path)
					curX, curY = x, y
					startX, startY = x, y
					first = false
				} else {
					_, _, _ = procGdipAddPathLine.Call(path, f32(float32(curX)), f32(float32(curY)), f32(float32(x)), f32(float32(y)))
					curX, curY = x, y
				}
			}
		case 'L', 'l':
			for {
				x, ok1 := nextNum()
				y, ok2 := nextNum()
				if !ok1 || !ok2 {
					break
				}
				if cmd == 'l' {
					x += curX
					y += curY
				}
				_, _, _ = procGdipAddPathLine.Call(path, f32(float32(curX)), f32(float32(curY)), f32(float32(x)), f32(float32(y)))
				curX, curY = x, y
			}
		case 'H', 'h':
			for {
				x, ok := nextNum()
				if !ok {
					break
				}
				if cmd == 'h' {
					x += curX
				}
				_, _, _ = procGdipAddPathLine.Call(path, f32(float32(curX)), f32(float32(curY)), f32(float32(x)), f32(float32(curY)))
				curX = x
			}
		case 'V', 'v':
			for {
				y, ok := nextNum()
				if !ok {
					break
				}
				if cmd == 'v' {
					y += curY
				}
				_, _, _ = procGdipAddPathLine.Call(path, f32(float32(curX)), f32(float32(curY)), f32(float32(curX)), f32(float32(y)))
				curY = y
			}
		case 'C', 'c':
			for {
				x1, ok1 := nextNum()
				y1, ok2 := nextNum()
				x2, ok3 := nextNum()
				y2, ok4 := nextNum()
				x, ok5 := nextNum()
				y, ok6 := nextNum()
				if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
					break
				}
				if cmd == 'c' {
					x1 += curX
					y1 += curY
					x2 += curX
					y2 += curY
					x += curX
					y += curY
				}
				_, _, _ = procGdipAddPathBezier.Call(path,
					f32(float32(curX)), f32(float32(curY)),
					f32(float32(x1)), f32(float32(y1)),
					f32(float32(x2)), f32(float32(y2)),
					f32(float32(x)), f32(float32(y)),
				)
				lastCtrlX, lastCtrlY = x2, y2
				curX, curY = x, y
			}
		case 'S', 's':
			for {
				x2, ok1 := nextNum()
				y2, ok2 := nextNum()
				x, ok3 := nextNum()
				y, ok4 := nextNum()
				if !ok1 || !ok2 || !ok3 || !ok4 {
					break
				}
				x1, y1 := curX, curY
				if lastCmd == 'C' || lastCmd == 'c' || lastCmd == 'S' || lastCmd == 's' {
					x1 = 2*curX - lastCtrlX
					y1 = 2*curY - lastCtrlY
				}
				if cmd == 's' {
					x2 += curX
					y2 += curY
					x += curX
					y += curY
				}
				_, _, _ = procGdipAddPathBezier.Call(path,
					f32(float32(curX)), f32(float32(curY)),
					f32(float32(x1)), f32(float32(y1)),
					f32(float32(x2)), f32(float32(y2)),
					f32(float32(x)), f32(float32(y)),
				)
				lastCtrlX, lastCtrlY = x2, y2
				curX, curY = x, y
			}
		case 'Q', 'q':
			for {
				qx, ok1 := nextNum()
				qy, ok2 := nextNum()
				x, ok3 := nextNum()
				y, ok4 := nextNum()
				if !ok1 || !ok2 || !ok3 || !ok4 {
					break
				}
				if cmd == 'q' {
					qx += curX
					qy += curY
					x += curX
					y += curY
				}
				c1x := curX + 2.0/3.0*(qx-curX)
				c1y := curY + 2.0/3.0*(qy-curY)
				c2x := x + 2.0/3.0*(qx-x)
				c2y := y + 2.0/3.0*(qy-y)
				_, _, _ = procGdipAddPathBezier.Call(path,
					f32(float32(curX)), f32(float32(curY)),
					f32(float32(c1x)), f32(float32(c1y)),
					f32(float32(c2x)), f32(float32(c2y)),
					f32(float32(x)), f32(float32(y)),
				)
				lastCtrlX, lastCtrlY = qx, qy
				curX, curY = x, y
			}
		case 'T', 't':
			for {
				x, ok1 := nextNum()
				y, ok2 := nextNum()
				if !ok1 || !ok2 {
					break
				}
				qx, qy := curX, curY
				if lastCmd == 'Q' || lastCmd == 'q' || lastCmd == 'T' || lastCmd == 't' {
					qx = 2*curX - lastCtrlX
					qy = 2*curY - lastCtrlY
				}
				if cmd == 't' {
					x += curX
					y += curY
				}
				c1x := curX + 2.0/3.0*(qx-curX)
				c1y := curY + 2.0/3.0*(qy-curY)
				c2x := x + 2.0/3.0*(qx-x)
				c2y := y + 2.0/3.0*(qy-y)
				_, _, _ = procGdipAddPathBezier.Call(path,
					f32(float32(curX)), f32(float32(curY)),
					f32(float32(c1x)), f32(float32(c1y)),
					f32(float32(c2x)), f32(float32(c2y)),
					f32(float32(x)), f32(float32(y)),
				)
				lastCtrlX, lastCtrlY = qx, qy
				curX, curY = x, y
			}
		case 'A', 'a':
			for {
				rx, ok1 := nextNum()
				ry, ok2 := nextNum()
				rot, ok3 := nextNum()
				large, ok4 := nextFlag()
				sweep, ok5 := nextFlag()
				x, ok6 := nextNum()
				y, ok7 := nextNum()
				if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
					break
				}
				if cmd == 'a' {
					x += curX
					y += curY
				}
				addArcAsBeziers(path, curX, curY, rx, ry, rot, large, sweep, x, y)
				curX, curY = x, y
			}
		case 'Z', 'z':
			_, _, _ = procGdipClosePathFigure.Call(path)
			curX, curY = startX, startY
		}
		lastCmd = cmd
	}
	return path
}

func isSVGCmd(c byte) bool {
	switch c {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'S', 's', 'Q', 'q', 'T', 't', 'A', 'a', 'Z', 'z':
		return true
	}
	return false
}

func tokenizeSVGPath(d string) []string {
	var out []string
	i := 0
	n := len(d)
	for i < n {
		c := d[i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if isSVGCmd(c) {
			out = append(out, string(c))
			i++
			continue
		}
		start := i
		if c == '+' || c == '-' {
			i++
		}
		hasDot := false
		hasExp := false
		for i < n {
			ch := d[i]
			if ch >= '0' && ch <= '9' {
				i++
			} else if ch == '.' && !hasDot && !hasExp {
				hasDot = true
				i++
			} else if (ch == 'e' || ch == 'E') && !hasExp {
				hasExp = true
				i++
				if i < n && (d[i] == '+' || d[i] == '-') {
					i++
				}
			} else {
				break
			}
		}
		if i > start {
			out = append(out, d[start:i])
		} else {
			i++
		}
	}
	return out
}

func addArcAsBeziers(path uintptr, x1, y1, rx, ry, phiDeg float64, largeArc, sweep bool, x2, y2 float64) {
	if x1 == x2 && y1 == y2 {
		return
	}
	if rx == 0 || ry == 0 {
		_, _, _ = procGdipAddPathLine.Call(path, f32(float32(x1)), f32(float32(y1)), f32(float32(x2)), f32(float32(y2)))
		return
	}
	rx = math.Abs(rx)
	ry = math.Abs(ry)
	phi := phiDeg * math.Pi / 180.0
	cosPhi := math.Cos(phi)
	sinPhi := math.Sin(phi)

	dx2 := (x1 - x2) / 2.0
	dy2 := (y1 - y2) / 2.0
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2

	rx2 := rx * rx
	ry2 := ry * ry
	x1p2 := x1p * x1p
	y1p2 := y1p * y1p

	lambda := x1p2/rx2 + y1p2/ry2
	if lambda > 1.0 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
		rx2 = rx * rx
		ry2 = ry * ry
	}

	num := math.Max(0, rx2*ry2-rx2*y1p2-ry2*x1p2)
	den := rx2*y1p2 + ry2*x1p2
	var coef float64
	if den > 0 {
		coef = math.Sqrt(num / den)
	}
	if largeArc == sweep {
		coef = -coef
	}
	cxp := coef * ((rx * y1p) / ry)
	cyp := coef * (-(ry * x1p) / rx)

	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2.0
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2.0

	theta1 := vecAngle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dTheta := vecAngle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	segs := int(math.Ceil(math.Abs(dTheta) / (math.Pi / 2.0)))
	if segs < 1 {
		segs = 1
	}
	delta := dTheta / float64(segs)
	t := theta1
	px, py := x1, y1
	alpha := 4.0 / 3.0 * math.Tan(delta/4.0)

	for s := 0; s < segs; s++ {
		t2 := t + delta
		cosT1, sinT1 := math.Cos(t), math.Sin(t)
		cosT2, sinT2 := math.Cos(t2), math.Sin(t2)

		e1x := cosT1 - alpha*sinT1
		e1y := sinT1 + alpha*cosT1
		e2x := cosT2 + alpha*sinT2
		e2y := sinT2 - alpha*cosT2

		c1x := cx + cosPhi*rx*e1x - sinPhi*ry*e1y
		c1y := cy + sinPhi*rx*e1x + cosPhi*ry*e1y
		c2x := cx + cosPhi*rx*e2x - sinPhi*ry*e2y
		c2y := cy + sinPhi*rx*e2x + cosPhi*ry*e2y
		nx := cx + cosPhi*rx*cosT2 - sinPhi*ry*sinT2
		ny := cy + sinPhi*rx*cosT2 + cosPhi*ry*sinT2

		_, _, _ = procGdipAddPathBezier.Call(path,
			f32(float32(px)), f32(float32(py)),
			f32(float32(c1x)), f32(float32(c1y)),
			f32(float32(c2x)), f32(float32(c2y)),
			f32(float32(nx)), f32(float32(ny)),
		)
		px, py = nx, ny
		t = t2
	}
}

func vecAngle(ux, uy, vx, vy float64) float64 {
	dot := ux*vx + uy*vy
	lenU := math.Hypot(ux, uy)
	lenV := math.Hypot(vx, vy)
	if lenU == 0 || lenV == 0 {
		return 0
	}
	c := dot / (lenU * lenV)
	if c < -1 {
		c = -1
	} else if c > 1 {
		c = 1
	}
	ang := math.Acos(c)
	if ux*vy-uy*vx < 0 {
		ang = -ang
	}
	return ang
}
