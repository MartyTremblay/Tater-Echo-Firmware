//go:build linux

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"time"

	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/linuxinput"
	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/show"
)

type actionMenuItem uint8

const (
	actionMenuNone actionMenuItem = iota
	actionMenuSatChat
	actionMenuCameras
	actionMenuClimate
)

var (
	menuBackdropInk = color.RGBA{13, 15, 20, 226}
	menuCardInk     = color.RGBA{28, 31, 38, 235}
	menuControlInk  = color.RGBA{34, 37, 45, 235}
	menuMutedInk    = color.RGBA{30, 32, 38, 220}
	menuOutlineInk  = color.RGBA{112, 119, 131, 150}
	menuSubtitleInk = color.RGBA{188, 195, 207, 255}
)

func actionMenuCloseBounds(width, height int, spot bool) image.Rectangle {
	if spot {
		return image.Rect(338, 58, 392, 112)
	}
	size := max(48, min(70, height/9))
	margin := max(28, width*55/1000)
	return image.Rect(width-margin-size, max(24, height*65/1000), width-margin, max(24, height*65/1000)+size)
}

func actionMenuBounds(width, height, index int, spot bool) image.Rectangle {
	if spot {
		switch index {
		case 0:
			return image.Rect(76, 142, 404, 218)
		case 1:
			return image.Rect(76, 229, 404, 305)
		case 2:
			return image.Rect(102, 318, 378, 365)
		default:
			return image.Rectangle{}
		}
	}
	marginX := max(28, width*55/1000)
	marginY := max(22, height*65/1000)
	panel := image.Rect(marginX, marginY, width-marginX, height-marginY)
	gap := max(10, width/80)
	top := panel.Min.Y + max(94, height*19/100)
	bottom := panel.Max.Y - max(42, height/16)
	cardWidth := (panel.Dx() - 2*max(18, width/55) - 2*gap) / 3
	left := panel.Min.X + max(18, width/55) + index*(cardWidth+gap)
	return image.Rect(left, top, left+cardWidth, bottom)
}

func actionMenuItemAt(point image.Point, width, height int, spot bool) actionMenuItem {
	for index, item := range []actionMenuItem{actionMenuSatChat, actionMenuCameras, actionMenuClimate} {
		if point.In(actionMenuBounds(width, height, index, spot)) {
			return item
		}
	}
	return actionMenuNone
}

func (r *renderer) handleActionMenuTouch(client *socketClient, event linuxinput.TouchEvent, width, height int, spot bool) {
	point := image.Pt(event.X, event.Y)
	if point.In(actionMenuCloseBounds(width, height, spot)) {
		r.menuOpen = false
		return
	}
	switch actionMenuItemAt(point, width, height, spot) {
	case actionMenuSatChat:
		r.menuOpen = false
		r.satChatPage = 0
		r.satChatOpen = true
	case actionMenuCameras:
		r.menuOpen = false
		r.cameraPage = 0
		r.cameraOpen = true
		client.sendCamera("camera.open", "")
	case actionMenuClimate:
		r.menuOpen = false
		r.climateOpen = true
	}
}

func (r *renderer) handleCameraTouch(client *socketClient, event linuxinput.TouchEvent, width, height int, spot bool) {
	point := image.Pt(event.X, event.Y)
	selected := r.state.Cameras != nil && r.state.Cameras.SelectedID != ""
	if event.Kind == linuxinput.Down && selected && r.state.Cameras.SelectedTalkback && point.In(r.cameraTalkBounds(width, height, spot)) {
		r.cameraTalkPressed = true
		client.sendCamera("camera.talk.start", r.state.Cameras.SelectedID)
		return
	}
	if event.Kind != linuxinput.Up {
		return
	}
	if r.cameraTalkPressed {
		r.cameraTalkPressed = false
		client.sendCamera("camera.talk.stop", "")
		return
	}
	if point.In(r.cameraCloseBounds(width, height, spot, selected)) {
		r.cameraOpen = false
		client.sendCamera("camera.close", "")
		return
	}
	if selected {
		if point.In(cameraBackBounds(width, height, spot)) {
			client.sendCamera("camera.close", "")
		}
		return
	}
	cameras := []show.Camera{}
	if r.state.Cameras != nil {
		cameras = r.state.Cameras.Items
	}
	perPage := cameraPageSize(width, height, spot)
	pages := max(1, (len(cameras)+perPage-1)/perPage)
	if point.In(cameraPreviousBounds(width, height, spot)) && pages > 1 {
		r.cameraPage = (r.cameraPage - 1 + pages) % pages
		return
	}
	if point.In(cameraNextBounds(width, height, spot)) && pages > 1 {
		r.cameraPage = (r.cameraPage + 1) % pages
		return
	}
	start := (r.cameraPage % pages) * perPage
	for index := 0; index < perPage && start+index < len(cameras); index++ {
		if point.In(cameraCardBounds(width, height, index, spot)) && cameras[start+index].Available {
			client.sendCamera("camera.select", cameras[start+index].ID)
			return
		}
	}
}

func (r *renderer) cameraCloseBounds(width, height int, spot, selected bool) image.Rectangle {
	if selected && !spot && r.isCrown() {
		return image.Rect(width-100, 28, width-52, 76)
	}
	return actionMenuCloseBounds(width, height, spot)
}

func (r *renderer) cameraTalkBounds(width, height int, spot bool) image.Rectangle {
	if !spot && r.isCrown() {
		return image.Rect(width/2-145, height-96, width/2+145, height-38)
	}
	return cameraTalkBounds(width, height, spot)
}

func (r *renderer) actionMenuTemperature() string {
	if climate := r.state.Climate; climate != nil && climate.Available {
		if climate.TargetTemperature != nil {
			return climateTemperature(climate.TargetTemperature, climate.TemperatureUnit)
		}
		if climate.CurrentTemperature != nil {
			return climateTemperature(climate.CurrentTemperature, climate.TemperatureUnit)
		}
	}
	if weather := r.state.Weather; weather != nil {
		return strings.TrimSpace(weather.IndoorTemperatureText)
	}
	return "--°"
}

func (r *renderer) drawActionMenuBackground(canvas *image.RGBA, now time.Time, accent color.RGBA, alpha uint8) {
	if wallpaper := r.activeWallpaper(now); wallpaper != nil {
		drawPreparedCover(canvas, canvas.Bounds(), wallpaper)
	} else {
		fillGradient(canvas, color.RGBA{12, 14, 19, 255}, mix(color.RGBA{12, 14, 19, 255}, accent, .06))
	}
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{R: 5, G: 6, B: 8, A: alpha}), image.Point{}, draw.Over)
}

func (r *renderer) drawActionMenu(canvas *image.RGBA, now time.Time, accent color.RGBA) {
	width, height := canvas.Rect.Dx(), canvas.Rect.Dy()
	r.drawActionMenuBackground(canvas, now, accent, 100)
	marginX := max(28, width*55/1000)
	marginY := max(22, height*65/1000)
	panel := image.Rect(marginX, marginY, width-marginX, height-marginY)
	radius := max(18, height/24)
	roundedRect(canvas, panel, radius, menuBackdropInk)
	outlineRoundedRect(canvas, panel, radius, 2, color.RGBA{accent.R, accent.G, accent.B, 195})

	large := height >= 700
	titleSize, subtitleSize, onlineSize := 30, 15, 14
	if large {
		titleSize, subtitleSize, onlineSize = 42, 20, 18
	}
	name := assistantFirstName(r.state.AssistantName)
	iconX := panel.Min.X + max(45, width/20)
	iconY := panel.Min.Y + max(45, height/14)
	drawMenuSparkle(canvas, iconX, iconY, max(16, height/26), accent)
	textX := iconX + max(36, width/28)
	titleY := panel.Min.Y + max(49, height/13)
	r.text(canvas, textX, titleY, strings.ToUpper(name)+" MENU", titleSize, true, color.White)
	r.text(canvas, textX, titleY+max(25, height/25), "What would you like to do?", subtitleSize, false, color.RGBA{187, 202, 219, 255})

	close := actionMenuCloseBounds(width, height, false)
	closeX, closeY := (close.Min.X+close.Max.X)/2, (close.Min.Y+close.Max.Y)/2
	circle(canvas, closeX, closeY, close.Dx()/2, menuControlInk)
	circleOutline(canvas, closeX, closeY, close.Dx()/2, 2, menuOutlineInk)
	drawClimateCloseGlyph(canvas, close, color.RGBA{255, 255, 255, 255})

	online := name + " Online"
	onlineWidth := textWidth(r.faces.regular[onlineSize], online)
	onlineRight := close.Min.X - max(24, width/45)
	dotX := onlineRight - onlineWidth - 14
	circle(canvas, dotX, closeY, max(4, onlineSize/3), color.RGBA{46, 231, 117, 255})
	r.text(canvas, dotX+13, closeY+onlineSize/3, online, onlineSize, false, color.RGBA{218, 231, 243, 255})

	headerLineY := actionMenuBounds(width, height, 0, false).Min.Y - max(15, height/35)
	line(canvas, panel.Min.X+max(18, width/55), headerLineY, panel.Max.X-max(18, width/55), headerLineY, 1, color.RGBA{128, 134, 145, 90})
	labels := []struct {
		title, subtitle string
		kind            actionMenuItem
	}{
		{"SAT CHAT", "Call another satellite", actionMenuSatChat},
		{"CAMERAS", "Live cameras & doorbells", actionMenuCameras},
		{"CLIMATE", "Thermostat & room temps", actionMenuClimate},
	}
	for index, label := range labels {
		bounds := actionMenuBounds(width, height, index, false)
		r.drawWideActionCard(canvas, bounds, label.kind, label.title, label.subtitle, accent, large)
	}
	hint := "Tap an action  ·  Hold anywhere to broadcast"
	r.centeredText(canvas, width/2, panel.Max.Y-max(13, height/40), hint, subtitleSize-2, false, color.RGBA{157, 176, 197, 255})
}

func (r *renderer) drawWideActionCard(canvas *image.RGBA, bounds image.Rectangle, kind actionMenuItem, title, subtitle string, accent color.RGBA, large bool) {
	radius := max(14, bounds.Dy()/12)
	roundedRect(canvas, bounds, radius, menuCardInk)
	outlineRoundedRect(canvas, bounds, radius, 2, menuOutlineInk)
	iconInk := accent
	iconX, iconY := bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()*31/100
	iconSize := max(18, min(35, bounds.Dy()/7))
	r.drawActionMenuGlyph(canvas, kind, iconX, iconY, iconSize, iconInk)
	titleSize, subtitleSize := 21, 13
	if large {
		titleSize, subtitleSize = 30, 18
	}
	r.centeredText(canvas, iconX, bounds.Min.Y+bounds.Dy()*58/100, title, titleSize, true, color.White)
	r.centeredText(canvas, iconX, bounds.Min.Y+bounds.Dy()*72/100, fitTextToWidth(subtitle, r.faces.regular[subtitleSize], bounds.Dx()-28), subtitleSize, false, menuSubtitleInk)
	if kind == actionMenuClimate {
		tempSize := 17
		if large {
			tempSize = 24
		}
		r.centeredText(canvas, iconX, bounds.Min.Y+bounds.Dy()*87/100, r.actionMenuTemperature(), tempSize, true, accent)
	}
	drawMenuChevron(canvas, bounds.Max.X-max(16, bounds.Dx()/12), bounds.Max.Y-max(17, bounds.Dy()/10), max(6, bounds.Dy()/28), color.RGBA{218, 230, 243, 235})
}

func (r *renderer) drawSpotActionMenu(canvas *image.RGBA, now time.Time, accent color.RGBA) {
	r.drawActionMenuBackground(canvas, now, accent, 94)
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{R: 2, G: 8, B: 17, A: 38}), image.Point{}, draw.Over)
	circle(canvas, 240, 240, 230, color.RGBA{13, 15, 20, 218})
	circleOutline(canvas, 240, 240, 234, 2, color.RGBA{accent.R, accent.G, accent.B, 205})
	name := assistantFirstName(r.state.AssistantName)
	drawMenuSparkle(canvas, 240, 43, 13, accent)
	r.centeredText(canvas, 240, 82, strings.ToUpper(name)+" MENU", 22, true, color.White)
	circle(canvas, 194, 107, 4, color.RGBA{46, 231, 117, 255})
	r.text(canvas, 205, 111, fitTextToWidth(name+" Online", r.faces.regular[11], 145), 11, false, color.RGBA{211, 226, 241, 255})

	close := actionMenuCloseBounds(480, 480, true)
	cx, cy := (close.Min.X+close.Max.X)/2, (close.Min.Y+close.Max.Y)/2
	circle(canvas, cx, cy, close.Dx()/2, menuControlInk)
	circleOutline(canvas, cx, cy, close.Dx()/2, 2, color.RGBA{accent.R, accent.G, accent.B, 190})
	drawClimateCloseGlyph(canvas, close, color.RGBA{255, 255, 255, 255})

	for index, item := range []struct {
		kind            actionMenuItem
		title, subtitle string
	}{
		{actionMenuSatChat, "SAT CHAT", "Satellite calls"},
		{actionMenuCameras, "CAMERAS", "Live view & talk"},
	} {
		bounds := actionMenuBounds(480, 480, index, true)
		roundedRect(canvas, bounds, 19, menuCardInk)
		outlineRoundedRect(canvas, bounds, 19, 2, menuOutlineInk)
		iconInk := accent
		r.drawActionMenuGlyph(canvas, item.kind, bounds.Min.X+45, (bounds.Min.Y+bounds.Max.Y)/2, 17, iconInk)
		r.text(canvas, bounds.Min.X+80, bounds.Min.Y+33, item.title, 17, true, color.White)
		r.text(canvas, bounds.Min.X+80, bounds.Min.Y+55, item.subtitle, 11, false, menuSubtitleInk)
		drawMenuChevron(canvas, bounds.Max.X-24, (bounds.Min.Y+bounds.Max.Y)/2, 7, color.RGBA{220, 231, 244, 235})
	}
	climate := actionMenuBounds(480, 480, 2, true)
	roundedRect(canvas, climate, 18, menuCardInk)
	outlineRoundedRect(canvas, climate, 18, 2, menuOutlineInk)
	r.drawActionMenuGlyph(canvas, actionMenuClimate, climate.Min.X+31, (climate.Min.Y+climate.Max.Y)/2, 12, accent)
	r.text(canvas, climate.Min.X+54, climate.Min.Y+29, "CLIMATE", 14, true, color.White)
	temp := r.actionMenuTemperature()
	tempWidth := textWidth(r.faces.bold[17], temp)
	r.text(canvas, climate.Max.X-tempWidth-34, climate.Min.Y+31, temp, 17, true, accent)
	drawMenuChevron(canvas, climate.Max.X-17, (climate.Min.Y+climate.Max.Y)/2, 6, color.RGBA{220, 231, 244, 235})
	r.centeredText(canvas, 240, 397, "HOLD ANYWHERE TO BROADCAST", 10, true, color.RGBA{157, 176, 197, 255})
	maskOutsideSpotCircle(canvas, 236)
}

func (r *renderer) drawActionMenuGlyph(canvas *image.RGBA, kind actionMenuItem, cx, cy, size int, ink color.RGBA) {
	switch kind {
	case actionMenuSatChat:
		drawSatChatNetworkGlyph(canvas, cx, cy, size, ink)
	case actionMenuCameras:
		drawMenuCameraGlyph(canvas, cx, cy, size, ink)
	case actionMenuClimate:
		r.drawWeatherMetricGlyph(canvas, cx, cy, size, "temperature", ink)
	}
}

func drawMenuCameraGlyph(canvas *image.RGBA, cx, cy, size int, ink color.RGBA) {
	body := image.Rect(cx-size, cy-size*2/3, cx+size, cy+size*2/3)
	outlineRoundedRect(canvas, body, max(3, size/4), max(2, size/7), ink)
	circleOutline(canvas, cx, cy, max(4, size/3), max(2, size/7), ink)
	line(canvas, cx-size/2, body.Min.Y, cx-size/3, body.Min.Y-size/4, max(2, size/8), ink)
	line(canvas, cx-size/3, body.Min.Y-size/4, cx+size/4, body.Min.Y-size/4, max(2, size/8), ink)
}

func drawMenuSparkle(canvas *image.RGBA, cx, cy, size int, ink color.RGBA) {
	line(canvas, cx, cy-size, cx, cy+size, max(2, size/7), ink)
	line(canvas, cx-size, cy, cx+size, cy, max(2, size/7), ink)
	line(canvas, cx-size*2/3, cy-size*2/3, cx+size*2/3, cy+size*2/3, max(1, size/10), ink)
	line(canvas, cx+size*2/3, cy-size*2/3, cx-size*2/3, cy+size*2/3, max(1, size/10), ink)
	circle(canvas, cx, cy, max(2, size/5), ink)
}

func drawMenuChevron(canvas *image.RGBA, cx, cy, size int, ink color.RGBA) {
	line(canvas, cx-size/2, cy-size, cx+size/2, cy, 2, ink)
	line(canvas, cx+size/2, cy, cx-size/2, cy+size, 2, ink)
}

func cameraCardBounds(width, height, index int, spot bool) image.Rectangle {
	if spot {
		return image.Rect(90, 120+index*51, 390, 164+index*51)
	}
	return satChatDirectoryLayoutFor(width, height).participantBounds(index)
}

func cameraPageSize(width, height int, spot bool) int {
	if spot {
		return 4
	}
	return satChatDirectoryLayoutFor(width, height).rows * 2
}

func cameraPreviousBounds(width, height int, spot bool) image.Rectangle {
	if spot {
		return image.Rect(126, 389, 230, 434)
	}
	return satChatPreviousBounds(width)
}
func cameraNextBounds(width, height int, spot bool) image.Rectangle {
	if spot {
		return image.Rect(250, 389, 354, 434)
	}
	return satChatNextBounds(width)
}
func cameraBackBounds(width, height int, spot bool) image.Rectangle {
	if spot {
		return image.Rect(64, 56, 124, 112)
	}
	return image.Rect(max(28, width/30), max(24, height/28), max(28, width/30)+150, max(24, height/28)+64)
}
func cameraTalkBounds(width, height int, spot bool) image.Rectangle {
	if spot {
		return image.Rect(151, 371, 329, 430)
	}
	return image.Rect(width/2-145, height-78, width/2+145, height-20)
}
func (r *renderer) cameraVideoBounds() image.Rectangle {
	width, height := r.showSize()
	if r.spotScreen {
		return image.Rect(48, 112, 432, 364)
	}
	return image.Rect(max(30, width/30), max(92, height/8), width-max(30, width/30), height-max(90, height/8))
}

func (r *renderer) cameraVideoActive() bool {
	return r.cameraOpen && r.state.Cameras != nil && r.state.Cameras.SelectedID != ""
}

func (r *renderer) cameraVideoRenderKey(accent color.RGBA) string {
	if r.state.Cameras == nil {
		return ""
	}
	return r.state.Cameras.SelectedID + "|" + r.state.Cameras.SelectedName + "|" +
		r.state.Cameras.SelectedRoom + "|" + r.state.Cameras.Error + "|" +
		fmt.Sprint(r.state.Cameras.SelectedTalkback, r.cameraTalkPressed, accent)
}

func (r *renderer) drawCameraVideoFrame(canvas *image.RGBA) image.Rectangle {
	bounds := r.cameraVideoBounds().Intersect(canvas.Rect)
	draw.Draw(canvas, bounds, image.NewUniform(color.Black), image.Point{}, draw.Src)
	if r.cameraFrameImage != nil {
		draw.Draw(canvas, bounds, r.cameraFrameImage, image.Point{}, draw.Src)
	}
	return bounds
}

func (r *renderer) drawCameraScreen(canvas *image.RGBA, now time.Time, accent color.RGBA) {
	width, height := canvas.Rect.Dx(), canvas.Rect.Dy()
	r.drawActionMenuBackground(canvas, now, accent, 112)
	panel := image.Rect(max(24, width/30), max(20, height/28), width-max(24, width/30), height-max(20, height/28))
	roundedRect(canvas, panel, max(18, height/28), menuBackdropInk)
	outlineRoundedRect(canvas, panel, max(18, height/28), 2, color.RGBA{accent.R, accent.G, accent.B, 200})
	r.drawCameraContents(canvas, now, accent, false)
}

func (r *renderer) drawSpotCameraScreen(canvas *image.RGBA, now time.Time, accent color.RGBA) {
	r.drawActionMenuBackground(canvas, now, accent, 112)
	circle(canvas, 240, 240, 230, color.RGBA{13, 15, 20, 218})
	circleOutline(canvas, 240, 240, 234, 2, color.RGBA{accent.R, accent.G, accent.B, 205})
	r.drawCameraContents(canvas, now, accent, true)
	maskOutsideSpotCircle(canvas, 236)
}

func (r *renderer) drawCameraContents(canvas *image.RGBA, now time.Time, accent color.RGBA, spot bool) {
	width, height := canvas.Rect.Dx(), canvas.Rect.Dy()
	selected := r.state.Cameras != nil && r.state.Cameras.SelectedID != ""
	close := r.cameraCloseBounds(width, height, spot, selected)
	cx, cy := (close.Min.X+close.Max.X)/2, (close.Min.Y+close.Max.Y)/2
	circle(canvas, cx, cy, close.Dx()/2, menuControlInk)
	circleOutline(canvas, cx, cy, close.Dx()/2, 2, color.RGBA{accent.R, accent.G, accent.B, 190})
	drawClimateCloseGlyph(canvas, close, color.RGBA{255, 255, 255, 255})
	cameras := r.state.Cameras
	if cameras != nil && cameras.SelectedID != "" {
		bounds := r.cameraVideoBounds()
		draw.Draw(canvas, bounds, image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, draw.Src)
		if r.cameraFrameImage != nil {
			draw.Draw(canvas, bounds, r.cameraFrameImage, image.Point{}, draw.Src)
		} else {
			r.centeredText(canvas, width/2, (bounds.Min.Y+bounds.Max.Y)/2, "CONNECTING TO CAMERA…", func() int {
				if spot {
					return 14
				}
				return max(17, height/32)
			}(), true, color.RGBA{220, 232, 245, 255})
		}
		back := cameraBackBounds(width, height, spot)
		roundedRect(canvas, back, back.Dy()/2, menuControlInk)
		r.text(canvas, back.Min.X+18, back.Min.Y+back.Dy()*2/3, "‹  BACK", func() int {
			if spot {
				return 12
			}
			return 17
		}(), true, color.White)
		nameSize := 17
		if spot {
			nameSize = 14
		} else if height >= 700 {
			nameSize = 25
		}
		label := cameras.SelectedName
		if cameras.SelectedRoom != "" {
			label += "  ·  " + cameras.SelectedRoom
		}
		r.centeredText(canvas, width/2, func() int {
			if spot {
				return 89
			}
			return max(58, height/13)
		}(), fitTextToWidth(label, r.faces.bold[nameSize], width/2), nameSize, true, color.White)
		if cameras.SelectedTalkback {
			talk := r.cameraTalkBounds(width, height, spot)
			ink := menuControlInk
			if r.cameraTalkPressed {
				ink = color.RGBA{accent.R, accent.G, accent.B, 235}
			}
			roundedRect(canvas, talk, talk.Dy()/2, ink)
			outlineRoundedRect(canvas, talk, talk.Dy()/2, 2, color.RGBA{accent.R, accent.G, accent.B, 220})
			r.centeredText(canvas, (talk.Min.X+talk.Max.X)/2, talk.Min.Y+talk.Dy()*2/3, "HOLD TO TALK", func() int {
				if spot {
					return 13
				}
				return 18
			}(), true, color.White)
		}
		if cameras.Error != "" {
			r.centeredText(canvas, width/2, bounds.Max.Y-14, fitTextToWidth(cameras.Error, r.faces.regular[13], bounds.Dx()-40), 13, false, color.RGBA{255, 166, 121, 255})
		}
		return
	}
	titleSize := 25
	if !spot && height >= 700 {
		titleSize = 42
	} else if !spot {
		titleSize = 30
	}
	if spot {
		r.drawActionMenuGlyph(canvas, actionMenuCameras, 240, 43, 13, accent)
		r.centeredText(canvas, 240, 85, "CAMERAS", titleSize, true, color.White)
	} else {
		layout := satChatDirectoryLayoutFor(width, height)
		headerIconX, headerIconY := layout.panel.Min.X+43, 63
		r.drawActionMenuGlyph(canvas, actionMenuCameras, headerIconX, headerIconY, 20, accent)
		r.text(canvas, layout.panel.Min.X+76, 70, "CAMERAS", titleSize, true, color.White)
		r.text(canvas, layout.panel.Min.X+76, 96, "Choose a camera to open live view", 15, false, menuSubtitleInk)
	}
	items := []show.Camera{}
	if cameras != nil {
		items = cameras.Items
	}
	if len(items) == 0 {
		r.drawActionMenuGlyph(canvas, actionMenuCameras, width/2, height/2-20, min(width, height)/10, color.RGBA{117, 151, 183, 255})
		r.centeredText(canvas, width/2, height/2+70, "NO CAMERA FEEDS AVAILABLE", func() int {
			if spot {
				return 16
			}
			return max(17, titleSize/2)
		}(), true, color.RGBA{224, 233, 244, 255})
		return
	}
	perPage := cameraPageSize(width, height, spot)
	pages := max(1, (len(items)+perPage-1)/perPage)
	r.cameraPage %= pages
	start := r.cameraPage * perPage
	end := min(len(items), start+perPage)
	if spot {
		r.centeredText(canvas, 240, 105, fmt.Sprintf("%d–%d OF %d", start+1, end, len(items)), 10, true, color.RGBA{215, 224, 237, 245})
	} else if pages > 1 {
		r.centeredText(canvas, width-190, 70, fmt.Sprintf("%d–%d OF %d", start+1, end, len(items)), 12, true, menuSubtitleInk)
	} else {
		r.centeredText(canvas, width-190, 70, fmt.Sprintf("%d AVAILABLE", len(items)), 12, true, menuSubtitleInk)
	}
	for index := 0; index < perPage && start+index < len(items); index++ {
		item := items[start+index]
		bounds := cameraCardBounds(width, height, index, spot)
		card := menuCardInk
		outline := color.RGBA{accent.R, accent.G, accent.B, 74}
		if !item.Available {
			card = menuMutedInk
			outline = color.RGBA{100, 106, 117, 80}
		}
		roundedRect(canvas, bounds, max(12, bounds.Dy()/4), card)
		outlineRoundedRect(canvas, bounds, max(12, bounds.Dy()/4), 1, outline)
		avatarRadius := min(26, bounds.Dy()/2-7)
		avatarX, centerY := bounds.Min.X+avatarRadius+18, (bounds.Min.Y+bounds.Max.Y)/2
		circle(canvas, avatarX, centerY, avatarRadius, color.RGBA{20, 22, 28, 190})
		circleOutline(canvas, avatarX, centerY, avatarRadius, 1, color.RGBA{163, 168, 178, 145})
		r.drawActionMenuGlyph(canvas, actionMenuCameras, avatarX, centerY, max(8, avatarRadius/2), color.RGBA{207, 212, 221, 235})
		statusX := avatarX + avatarRadius + 20
		circle(canvas, statusX, centerY-8, 6, func() color.RGBA {
			if item.Available {
				return color.RGBA{42, 233, 113, 255}
			}
			return color.RGBA{126, 137, 150, 255}
		}())
		nameSize, subSize := 19, 12
		if spot {
			nameSize, subSize = 16, 9
		} else if height >= 700 {
			nameSize, subSize = 19, 12
		}
		nameLeft := statusX + 20
		r.text(canvas, nameLeft, centerY-2, fitTextToWidth(item.Name, r.faces.bold[nameSize], max(90, bounds.Max.X-nameLeft-116)), nameSize, true, color.White)
		sub := "LIVE VIEW"
		if item.Room != "" {
			sub = strings.ToUpper(item.Room)
		}
		if item.Talkback {
			sub += "  ·  TWO-WAY AUDIO"
		}
		r.text(canvas, nameLeft, centerY+20, fitTextToWidth(sub, r.faces.bold[subSize], max(90, bounds.Max.X-nameLeft-95)), subSize, true, menuSubtitleInk)
		iconInk := accent
		if item.Talkback {
			drawSatChatMicGlyph(canvas, bounds.Max.X-54, centerY-4, 10, iconInk)
		}
		drawMenuCameraGlyph(canvas, bounds.Max.X-func() int {
			if item.Talkback {
				return 94
			}
			return 54
		}(), centerY-4, 10, iconInk)
		drawMenuChevron(canvas, bounds.Max.X-24, (bounds.Min.Y+bounds.Max.Y)/2, 7, color.RGBA{219, 232, 244, 235})
	}
	if pages > 1 {
		for index, button := range []struct {
			bounds image.Rectangle
			label  string
		}{{cameraPreviousBounds(width, height, spot), "‹ PREV"}, {cameraNextBounds(width, height, spot), "NEXT ›"}} {
			buttonInk := color.RGBA{accent.R, accent.G, accent.B, 220}
			if (index == 0 && r.cameraPage == 0) || (index == 1 && r.cameraPage == pages-1) {
				buttonInk = color.RGBA{92, 97, 106, 160}
			}
			roundedRect(canvas, button.bounds, button.bounds.Dy()/2, menuControlInk)
			outlineRoundedRect(canvas, button.bounds, button.bounds.Dy()/2, 1, buttonInk)
			r.centeredText(canvas, (button.bounds.Min.X+button.bounds.Max.X)/2, button.bounds.Min.Y+button.bounds.Dy()*2/3, button.label, func() int {
				if spot {
					return 11
				}
				return 12
			}(), true, buttonInk)
		}
	}
}
