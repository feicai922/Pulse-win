package ui

import (
	"testing"

	"pulse-windows/internal/model"
	"pulse-windows/internal/win32"
)

func TestUnitFlareTurnEndpoints(t *testing.T) {
	if len(unitFlareTurn) != 33 {
		t.Fatalf("expected 33 points, got %d", len(unitFlareTurn))
	}
	first := unitFlareTurn[0]
	last := unitFlareTurn[len(unitFlareTurn)-1]
	if first.X != 0 || first.Y != 0 {
		t.Fatalf("unexpected start point: %+v", first)
	}
	if last.X < 0.99 || last.X > 1.01 || last.Y < 0.99 || last.Y > 1.01 {
		t.Fatalf("unexpected end point: %+v", last)
	}
}

func TestBuildPulsePaths(t *testing.T) {
	win32.InitGDIPlus()
	defer win32.ShutdownGDIPlus()

	for _, dock := range []model.PanelDock{model.DockLeft, model.DockRight, model.DockTop, model.DockBottom, model.DockFloatVertical} {
		for _, round := range []bool{false, true} {
			p := BuildDockBerthPath(dock, round, 1.0, 0, 0, 64, 280, 1.0)
			if p == 0 {
				t.Fatalf("BuildDockBerthPath returned 0 for dock=%s round=%v", dock, round)
			}
			win32.DeletePath(p)
		}
	}

	b := BuildUsageBubblePath(model.DockRight, false, false, false, 0, 0, 250, 180, 20, 40, 20, 90)
	if b == 0 {
		t.Fatal("BuildUsageBubblePath returned 0")
	}
	win32.DeletePath(b)
}
