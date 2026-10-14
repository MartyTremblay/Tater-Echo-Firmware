//go:build spot

package camera

import (
	"image"
	"math"
)

const taterEchoVideoWidth, taterEchoVideoHeight = 320, 180

const taterGammaSteps = 4096

var taterVideoGamma = func() [taterGammaSteps + 1]uint8 {
	var table [taterGammaSteps + 1]uint8
	for index := range table {
		table[index] = uint8(math.Pow(float64(index)/taterGammaSteps, 1/2.2)*255 + 0.5)
	}
	return table
}()

// TaterEchoVideo develops the Rook camera directly at the size used by an
// Echo-to-Echo Sat Chat call. Frame.Image first demosaics all 640x480 pixels
// and the camera helper then discards most of them while cropping/downscaling;
// doing the same bilinear Bayer reconstruction at 320x180 removes that work
// without changing snapshots or higher-quality mobile calls.
func (f *Frame) TaterEchoVideo(reuse *image.RGBA) *image.RGBA {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.raw == nil {
		return nil
	}
	return renderTaterEchoVideo(f.raw, f.level(), reuse)
}

func renderTaterEchoVideo(raw []byte, t tone, reuse *image.RGBA) *image.RGBA {
	if len(raw) < sensorW*sensorH {
		return nil
	}
	if reuse == nil || reuse.Bounds().Dx() != taterEchoVideoWidth || reuse.Bounds().Dy() != taterEchoVideoHeight {
		reuse = image.NewRGBA(image.Rect(0, 0, taterEchoVideoWidth, taterEchoVideoHeight))
	}
	// TECHO5's full-size developer builds each channel LUT with 1,024 pow()
	// calls. That is acceptable for snapshots but costs hundreds of milliseconds
	// per frame on Rook. Preserve the curve with a one-time gamma table.
	lr, lg, lb := taterVideoLUT(t, t.gainR), taterVideoLUT(t, 1), taterVideoLUT(t, t.gainB)
	// Center-crop 640x480 to 640x360. Starting on the even row preserves the
	// RGGB phase, and each 2x2 Bayer cell becomes one complete output pixel.
	cropY := (sensorH - taterEchoVideoHeight*2) / 2
	for y := 0; y < taterEchoVideoHeight; y++ {
		sourceY := cropY + y*2
		row := sourceY * sensorW
		up := row - sensorW
		down := row + sensorW
		destination := y * reuse.Stride
		for x := 0; x < taterEchoVideoWidth; x++ {
			sourceX := x * 2
			left := sourceX - 1
			right := sourceX + 1
			if left < 0 {
				left = 1
			}
			if right >= sensorW {
				right = sensorW - 2
			}
			center := row + sourceX
			r := uint32(raw[center]) * 4
			g := uint32(raw[up+sourceX]) + uint32(raw[down+sourceX]) + uint32(raw[row+left]) + uint32(raw[row+right])
			b := uint32(raw[up+left]) + uint32(raw[up+right]) + uint32(raw[down+left]) + uint32(raw[down+right])
			pixel := destination + x*4
			reuse.Pix[pixel+0] = lr[min(r, 1023)]
			reuse.Pix[pixel+1] = lg[min(g, 1023)]
			reuse.Pix[pixel+2] = lb[min(b, 1023)]
			reuse.Pix[pixel+3] = 255
		}
	}
	return reuse
}

func taterVideoLUT(t tone, gain float64) *[1024]uint8 {
	var result [1024]uint8
	span := float64(t.white - t.black)
	if span < 1 {
		span = 1
	}
	for value := range result {
		normalized := (float64(value)/4*gain - float64(t.black)) / span
		index := int(normalized*taterGammaSteps + 0.5)
		index = min(max(index, 0), taterGammaSteps)
		result[value] = taterVideoGamma[index]
	}
	return &result
}
