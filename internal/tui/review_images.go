package tui

import (
	"fmt"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/reviewdiff"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/textsafe"
)

func (m *Model) paintReviewImages(g *grid.Grid, r reviewdiff.DiffRect) {
	d := m.review.model.Diff
	if d == nil || m.img.off || d.Images == nil {
		m.review.imageDiff = nil
		m.review.imageRender = [2]mosaicRender{}
		return
	}
	h := m.review.model.DiffViewportRows(r)
	if r.W < 3 || h < 2 {
		return
	}
	if m.review.imageDiff != d.Images {
		m.review.imageDiff = d.Images
		m.review.imageRender = [2]mosaicRender{}
	}
	st := m.review.styles
	for y := r.Y; y < r.Y+h; y++ {
		g.Fill(r.X, r.X+r.W, y, st.Text)
	}
	left := (r.W - 1) / 2
	areas := [2]imageRegion{
		{x: r.X, y: r.Y, w: left, h: h},
		{x: r.X + left + 1, y: r.Y, w: r.W - left - 1, h: h},
	}
	if r.W < 36 && h >= 5 {
		top := (h - 1) / 2
		areas = [2]imageRegion{
			{x: r.X, y: r.Y, w: r.W, h: top},
			{x: r.X, y: r.Y + top + 1, w: r.W, h: h - top - 1},
		}
		g.Fill(r.X, r.X+r.W, r.Y+top, st.HeaderBar)
	} else {
		for y := r.Y; y < r.Y+h; y++ {
			g.Set(r.X+left, y, "│", st.Muted)
		}
	}
	labels := [2]string{d.BaseRef, "Working Tree"}
	switch d.Scope {
	case reviewdiff.ScopeStaged:
		labels = [2]string{"HEAD", "Index"}
	case reviewdiff.ScopeUnstaged:
		labels[0] = "Index"
	}
	cw, ch := m.img.cellSize()
	for i, area := range areas {
		side := d.Images[i]
		label := labels[i]
		if side.Size.X > 0 {
			label += fmt.Sprintf(" · %d×%d", side.Size.X, side.Size.Y)
		}
		g.Fill(area.x, area.x+area.w, area.y, st.HeaderBar)
		g.Put(area.x, area.y, area.x+area.w, space.End(textsafe.Clean(label), area.w), st.HeaderBar)
		if side.Pixels == nil {
			msg := side.Error
			if msg == "" {
				msg = "Image absent"
			}
			g.Put(area.x, area.y+1, area.x+area.w, space.End(textsafe.Clean(msg), area.w), st.Muted)
			continue
		}
		body := imageRegion{x: area.x, y: area.y + 1, w: area.w, h: area.h - 1}
		w, h := min(body.w, 160), min(body.h, 80)
		body.x, body.y = body.x+(body.w-w)/2, body.y+(body.h-h)/2
		body.w, body.h = w, h
		fit := imageFit(body, side.Size.X, side.Size.Y, cw, ch)
		cells := m.review.imageRender[i].pictureCells(side.Pixels, fit.w, fit.h)
		for y := range fit.h {
			for x, cell := range cells[y*fit.w : (y+1)*fit.w] {
				g.Set(fit.x+x, fit.y+y, cell.Text, cell.Style)
			}
		}
	}
}
