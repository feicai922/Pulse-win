package ui

import (
	"math"

	"pulse-windows/internal/model"
	"pulse-windows/internal/win32"
)

// Precomputed 32-step unit fluidTurn from Pulse's DropletTether.swift (flareRamp = 0.5).
var unitFlareTurn = computeUnitTurn(0.5, 32)

func computeUnitTurn(ramp float64, steps int) []win32.PointF {
	raw := make([]win32.PointF, steps+1)
	var px, py float64
	ds := 1.0 / float64(steps)
	for s := 0; s < steps; s++ {
		u := (float64(s) + 0.5) * ds
		eased := (1.0 - math.Cos(math.Pi*u)) / 2.0
		heading := (math.Pi / 2.0) * ((1.0-ramp)*u + ramp*eased)
		px += math.Cos(heading) * ds
		py += math.Sin(heading) * ds
		raw[s+1] = win32.PointF{X: float32(px), Y: float32(py)}
	}
	ex, ey := raw[steps].X, raw[steps].Y
	for i := range raw {
		raw[i].X /= ex
		raw[i].Y /= ey
	}
	return raw
}

func fluidTurnPoints(from win32.PointF, alongX, alongY float32, to win32.PointF, thenX, thenY float32) []win32.PointF {
	dx := to.X - from.X
	dy := to.Y - from.Y
	first := dx*alongX + dy*alongY
	second := dx*thenX + dy*thenY
	pts := make([]win32.PointF, 0, len(unitFlareTurn))
	for _, u := range unitFlareTurn {
		pts = append(pts, win32.PointF{
			X: from.X + alongX*u.X*first + thenX*u.Y*second,
			Y: from.Y + alongY*u.X*first + thenY*u.Y*second,
		})
	}
	return pts
}

func squircleCornerPoints(cx, cy, r, startX, startY, endX, endY float32) []win32.PointF {
	const steps = 28
	const exp = 2.0 / 4.0 // 4th-order superellipse
	pts := make([]win32.PointF, 0, steps)
	for i := 1; i <= steps; i++ {
		t := (float64(i) / float64(steps)) * (math.Pi / 2.0)
		along := float32(math.Pow(math.Cos(t), exp))
		across := float32(math.Pow(math.Sin(t), exp))
		pts = append(pts, win32.PointF{
			X: cx + r*(startX*along+endX*across),
			Y: cy + r*(startY*along+endY*across),
		})
	}
	return pts
}

// BuildDockBerthPath builds Pulse's DockBerthShape: concave fluid flares fused into the screen edge
// when docked, or a true capsule pill when floating.
func BuildDockBerthPath(dock model.PanelDock, usesRoundEnds bool, openness float32, x, y, w, h, s float32) uintptr {
	if openness < 0 {
		openness = 0
	} else if openness > 1 {
		openness = 1
	}
	if !dock.IsDocked() {
		r := float32(math.Min(float64(w), float64(h))) / 2.0
		return win32.NewRoundRectPath(x, y, w, h, r)
	}

	// Canonical dimensions are computed facing RIGHT (width = across, height = along).
	cw, ch := w, h
	if dock == model.DockTop || dock == model.DockBottom {
		cw, ch = h, w
	}

	baseCorner := 26 * s
	baseFlareH := 24 * s
	baseFlareW := 38 * s
	if usesRoundEnds {
		baseCorner = cw / 2
		baseFlareH = baseCorner
		baseFlareW = cw - baseCorner
	}
	collapsedW := 6 * s

	f := minF(baseFlareH*openness, ch/2)
	cR := collapsedW + (baseCorner-collapsedW)*openness
	r := maxF(minF(cR, minF(cw, (ch-f*2)/2)), 0)
	fw := maxF(minF(baseFlareW*openness, cw-r), 0)

	var pts []win32.PointF
	// Top body edge left -> right
	pts = append(pts, win32.PointF{X: r, Y: f})
	if cw-fw > r {
		pts = append(pts, win32.PointF{X: cw - fw, Y: f})
	}
	// Top concave flare sweeping up into right screen edge (x = cw, y = 0)
	if fw > 0.5 && f > 0.5 {
		pts = append(pts, fluidTurnPoints(
			win32.PointF{X: cw - fw, Y: f}, 1, 0,
			win32.PointF{X: cw, Y: 0}, 0, -1,
		)...)
	} else {
		pts = append(pts, win32.PointF{X: cw, Y: 0})
	}
	// Flush along screen edge
	pts = append(pts, win32.PointF{X: cw, Y: ch})
	// Bottom concave flare sweeping back to body's bottom edge
	if fw > 0.5 && f > 0.5 {
		pts = append(pts, fluidTurnPoints(
			win32.PointF{X: cw, Y: ch}, 0, -1,
			win32.PointF{X: cw - fw, Y: ch - f}, -1, 0,
		)...)
	} else {
		pts = append(pts, win32.PointF{X: cw - fw, Y: ch - f})
	}
	// Bottom body edge right -> left
	pts = append(pts, win32.PointF{X: r, Y: ch - f})

	// Far corners (bottom-left -> left edge -> top-left)
	if r > 0.5 {
		if usesRoundEnds {
			pts = append(pts, squircleCornerExp2(r, ch-f-r, r, 0, 1, -1, 0)...)
			pts = append(pts, win32.PointF{X: 0, Y: f + r})
			pts = append(pts, squircleCornerExp2(r, f+r, r, -1, 0, 0, -1)...)
		} else {
			pts = append(pts, squircleCornerPoints(r, ch-f-r, r, 0, 1, -1, 0)...)
			pts = append(pts, win32.PointF{X: 0, Y: f + r})
			pts = append(pts, squircleCornerPoints(r, f+r, r, -1, 0, 0, -1)...)
		}
	} else {
		pts = append(pts, win32.PointF{X: 0, Y: ch - f}, win32.PointF{X: 0, Y: f})
	}

	// Transform canonical facing-right points into actual dock edge & position
	for i, p := range pts {
		switch dock {
		case model.DockRight:
			pts[i] = win32.PointF{X: x + p.X, Y: y + p.Y}
		case model.DockLeft:
			pts[i] = win32.PointF{X: x + cw - p.X, Y: y + p.Y}
		case model.DockTop:
			pts[i] = win32.PointF{X: x + p.Y, Y: y + p.X}
		case model.DockBottom:
			pts[i] = win32.PointF{X: x + p.Y, Y: y + cw - p.X}
		}
	}

	path := win32.NewPath(true)
	win32.PathStartFigure(path)
	win32.PathAddLines(path, pts)
	win32.PathCloseFigure(path)
	return path
}

func squircleCornerExp2(cx, cy, r, startX, startY, endX, endY float32) []win32.PointF {
	const steps = 24
	pts := make([]win32.PointF, 0, steps)
	for i := 1; i <= steps; i++ {
		t := (float64(i) / float64(steps)) * (math.Pi / 2.0)
		along := float32(math.Cos(t))
		across := float32(math.Sin(t))
		pts = append(pts, win32.PointF{
			X: cx + r*(startX*along+endX*across),
			Y: cy + r*(startY*along+endY*across),
		})
	}
	return pts
}

// BuildUsageBubblePath builds Pulse's UsageBubbleShape: rounded body + curved droplet pointer tail as one path.
func BuildUsageBubblePath(dock model.PanelDock, cardOpensRight, cardOpensDown, usesRoundEnds bool,
	bx, by, bodyW, bodyH, ptrW, ptrH, cornerR, pointerCenter float32) uintptr {

	// Build body rounded rect path
	path := win32.NewRoundRectPath(bx, by, bodyW, bodyH, cornerR)
	if path == 0 {
		return 0
	}

	nearAlong, nearAcross := float32(0.24), float32(0.44)
	farAlong, farAcross := float32(0.55), float32(0.24)
	if usesRoundEnds {
		nearAlong, nearAcross = 0.0, 0.50
		farAlong, farAcross = 0.55, 0.22
	}

	if !dock.IsHorizontal() {
		r := minF(cornerR, bodyH/2)
		half := minF(ptrH/2, maxF(bodyH/2-r, 0))
		ring := clampF(pointerCenter, 0, bodyH)
		inset := r + half
		centre := clampF(ring, inset, maxF(bodyH-inset, inset))
		tip := clampF(ring, centre-half, centre+half)

		var baseX, tipX float32
		if cardOpensRight {
			baseX = bx
			tipX = bx - ptrW
		} else {
			baseX = bx + bodyW
			tipX = bx + bodyW + ptrW
		}
		reach := tipX - baseX
		sweep := half
		if cardOpensRight {
			sweep = -half
		}

		win32.PathStartFigure(path)
		win32.PathAddBezier(path,
			baseX, by+centre-sweep,
			baseX+reach*nearAlong, by+centre-sweep*nearAcross,
			baseX+reach*farAlong, by+clampF(tip-sweep*farAcross, 0, bodyH),
			tipX, by+tip,
		)
		win32.PathAddBezier(path,
			tipX, by+tip,
			baseX+reach*farAlong, by+clampF(tip+sweep*farAcross, 0, bodyH),
			baseX+reach*nearAlong, by+centre+sweep*nearAcross,
			baseX, by+centre+sweep,
		)
		win32.PathAddLine(path, baseX, by+centre+sweep, baseX-reach*0.08, by+centre+sweep)
		win32.PathAddLine(path, baseX-reach*0.08, by+centre+sweep, baseX-reach*0.08, by+centre-sweep)
		win32.PathCloseFigure(path)
		return path
	}

	// Horizontal dock: pointer aims up or down
	r := minF(cornerR, bodyW/2)
	half := minF(ptrH/2, maxF(bodyW/2-r, 0))
	ring := clampF(pointerCenter, 0, bodyW)
	inset := r + half
	centre := clampF(ring, inset, maxF(bodyW-inset, inset))
	tip := clampF(ring, centre-half, centre+half)

	var baseY, tipY float32
	if cardOpensDown {
		baseY = by
		tipY = by - ptrW
	} else {
		baseY = by + bodyH
		tipY = by + bodyH + ptrW
	}
	reach := tipY - baseY
	sweep := half
	if !cardOpensDown {
		sweep = -half
	}

	win32.PathStartFigure(path)
	win32.PathAddBezier(path,
		bx+centre-sweep, baseY,
		bx+centre-sweep*nearAcross, baseY+reach*nearAlong,
		bx+clampF(tip-sweep*farAcross, 0, bodyW), baseY+reach*farAlong,
		bx+tip, tipY,
	)
	win32.PathAddBezier(path,
		bx+tip, tipY,
		bx+clampF(tip+sweep*farAcross, 0, bodyW), baseY+reach*farAlong,
		bx+centre+sweep*nearAcross, baseY+reach*nearAlong,
		bx+centre+sweep, baseY,
	)
	win32.PathAddLine(path, bx+centre+sweep, baseY, bx+centre+sweep, baseY-reach*0.08)
	win32.PathAddLine(path, bx+centre+sweep, baseY-reach*0.08, bx+centre-sweep, baseY-reach*0.08)
	win32.PathCloseFigure(path)
	return path
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
