//go:build !spot && !dot

package camera

import "image"

const taterEchoVideoWidth, taterEchoVideoHeight = 320, 180

// TaterEchoVideo develops only the pixels needed by a Sat Chat frame. The
// normal live image expands every Bayer cell to 640x360 (or 800x600) before
// the helper downsizes it. Reading the required packed samples directly cuts
// both conversion work and allocation pressure at the camera source.
func (f *Frame) TaterEchoVideo(reuse *image.RGBA) *image.RGBA {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.raw == nil {
		return nil
	}
	return renderTaterEchoVideo(f.raw, f.level(), reuse)
}

func renderTaterEchoVideo(raw []byte, level tone, reuse *image.RGBA) *image.RGBA {
	if len(raw) < frameBytes {
		return nil
	}
	if reuse == nil || reuse.Bounds().Dx() != taterEchoVideoWidth || reuse.Bounds().Dy() != taterEchoVideoHeight {
		reuse = image.NewRGBA(image.Rect(0, 0, taterEchoVideoWidth, taterEchoVideoHeight))
	}

	// Width/Height are measured in Bayer cells. Crop the source cells to 16:9
	// before sampling so cronos retains the same uncropped call composition as
	// the former post-render scaler; OV9734 Checkers/Crown are already 16:9.
	cropWidth := Width
	cropHeight := cropWidth * 9 / 16
	if cropHeight > Height {
		cropHeight = Height
		cropWidth = cropHeight * 16 / 9
	}
	cropX := (Width - cropWidth) / 2
	cropY := (Height - cropHeight) / 2
	luts := level.tables()
	for y := 0; y < taterEchoVideoHeight; y++ {
		cellY := cropY + y*cropHeight/taterEchoVideoHeight
		sensorY := cellY * 2
		row0 := raw[sensorY*bytesPerLine : (sensorY+1)*bytesPerLine]
		row1 := raw[(sensorY+1)*bytesPerLine : (sensorY+2)*bytesPerLine]
		destination := y * reuse.Stride
		for x := 0; x < taterEchoVideoWidth; x++ {
			cellX := cropX + x*cropWidth/taterEchoVideoWidth
			sensorX := cellX * 2
			r := packed10Sample(row0, sensorX)
			g := packed10Sample(row0, sensorX+1) + packed10Sample(row1, sensorX)
			b := packed10Sample(row1, sensorX+1)
			if sensor.blueFirst {
				r, b = b, r
			}
			pixel := destination + x*4
			reuse.Pix[pixel+0] = luts[0][r<<1]
			reuse.Pix[pixel+1] = luts[1][g]
			reuse.Pix[pixel+2] = luts[2][b<<1]
			reuse.Pix[pixel+3] = 255
		}
	}
	return reuse
}

func packed10Sample(line []byte, pixel int) uint16 {
	group := pixel / 4 * 5
	b0, b1 := uint16(line[group]), uint16(line[group+1])
	switch pixel & 3 {
	case 0:
		return b0 | (b1&3)<<8
	case 1:
		return b1>>2 | (uint16(line[group+2])&0x0f)<<6
	case 2:
		b2, b3 := uint16(line[group+2]), uint16(line[group+3])
		return b2>>4 | (b3&0x3f)<<4
	default:
		return uint16(line[group+3])>>6 | uint16(line[group+4])<<2
	}
}
