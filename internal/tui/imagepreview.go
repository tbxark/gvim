package tui

import (
	"image"
	"path"
	"strings"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// ImagePreviewMaxBytes bounds each Markdown or Git image read.
const ImagePreviewMaxBytes = 16 << 20

// ImagePreviewPath reports whether the path names a supported preview format.
func ImagePreviewPath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// DecodeImagePreview decodes a bounded image for an embedded preview. It returns
// the original dimensions alongside the downscaled raster used to paint cells.
func DecodeImagePreview(data []byte) (image.Image, image.Point, error) {
	if len(data) > ImagePreviewMaxBytes {
		return nil, image.Point{}, errImageTooLarge
	}
	return decodeMosaic(data, 16<<20)
}

func (r *mosaicRender) pictureCells(img image.Image, width, height int) []grid.Cell {
	if r.w != width || r.h != height || len(r.cells) != width*height {
		*r = mosaicRender{w: width, h: height, cells: mosaicCells(img, width, height)}
	}
	return r.cells
}
