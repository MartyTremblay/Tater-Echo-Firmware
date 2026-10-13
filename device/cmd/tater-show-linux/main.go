//go:build linux

// tater-show-linux is the native Checkers, Crown, and Rook renderer. The Tater daemon remains
// the source of truth; this process handles screen, touch, and loopback commands.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/linuxinput"
	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/linuxscreen"
	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/show"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

var (
	version = "dev"
	target  = "checkers"
)

type socketClient struct {
	mu     sync.Mutex
	writer *bufio.Writer
	states chan show.Snapshot
}

func newSocketClient() *socketClient {
	return &socketClient{states: make(chan show.Snapshot, 4)}
}

func (client *socketClient) run(ctx context.Context) {
	for ctx.Err() == nil {
		connection, err := (&net.Dialer{Timeout: 1200 * time.Millisecond}).DialContext(ctx, "tcp", show.DefaultAddress)
		if err != nil {
			wait(ctx, 1200*time.Millisecond)
			continue
		}
		client.mu.Lock()
		client.writer = bufio.NewWriter(connection)
		client.mu.Unlock()
		client.send("screen.ready")
		scanner := bufio.NewScanner(connection)
		scanner.Buffer(make([]byte, 64*1024), 256*1024)
		for scanner.Scan() {
			var snapshot show.Snapshot
			if err := json.Unmarshal(scanner.Bytes(), &snapshot); err != nil || snapshot.Protocol != show.ProtocolVersion || snapshot.Type != "snapshot" {
				continue
			}
			select {
			case client.states <- snapshot:
			default:
				select {
				case <-client.states:
				default:
				}
				client.states <- snapshot
			}
		}
		client.mu.Lock()
		client.writer = nil
		client.mu.Unlock()
		_ = connection.Close()
		wait(ctx, 1200*time.Millisecond)
	}
}

func (client *socketClient) send(action string) {
	client.sendCommand(show.Command{Protocol: show.ProtocolVersion, Type: "command", Action: action})
}

func (client *socketClient) sendDiagnostics(diagnostics show.ScreenDiagnostics) {
	client.sendCommand(show.Command{
		Protocol: show.ProtocolVersion, Type: "command", Action: "screen.diagnostics",
		Diagnostics: &diagnostics,
	})
}

func (client *socketClient) sendCommand(command show.Command) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.writer == nil {
		return
	}
	if err := json.NewEncoder(client.writer).Encode(command); err == nil {
		_ = client.writer.Flush()
	}
}

func wait(ctx context.Context, duration time.Duration) {
	select {
	case <-time.After(duration):
	case <-ctx.Done():
	}
}

type faceSet struct {
	regular map[int]font.Face
	bold    map[int]font.Face
}

func newFaceSet() (*faceSet, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	result := &faceSet{regular: map[int]font.Face{}, bold: map[int]font.Face{}}
	for _, size := range []int{12, 14, 15, 16, 18, 20, 22, 24, 26, 30, 36, 48, 64, 74, 82, 96} {
		result.regular[size], err = opentype.NewFace(regular, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, err
		}
		result.bold[size], err = opentype.NewFace(bold, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

type renderer struct {
	faces             *faceSet
	spotScreen        bool
	target            string
	screenWidth       int
	screenHeight      int
	state             show.Snapshot
	received          time.Time
	animationStart    time.Time
	notificationID    string
	notificationImage image.Image
	lastEventID       string
	lastEventImage    image.Image
	mediaID           string
	mediaImage        image.Image
	intercomPressed   bool
	timerPressed      bool
	intercomIcon      *image.RGBA
	connectingBase    *image.RGBA
	spotBase          *image.RGBA
	spotBaseAccent    color.RGBA
	spotTouchDown     bool
	spotTouchAt       time.Time
	spotTimerDown     bool
	spotReplyGlow     float64
	checkersReplyGlow float64
	musicLevels       [12]float64
	musicLevelsAt     time.Time
	musicLevelsMedia  string
	musicLevelsReady  bool
	checkersBase      *image.RGBA
	checkersBaseKey   string
	checkersCanvas    *image.RGBA
	spotFrameBase     *image.RGBA
	spotFrameBaseKey  string
	spotFrameCanvas   *image.RGBA
	crownContext      *image.RGBA
	crownBase         *image.RGBA
	crownBaseKey      string
	crownCanvas       *image.RGBA
	presentRegions    []image.Rectangle
	touchDowns        uint64
	touchMoves        uint64
	touchUps          uint64
	lastTouchX        int
	lastTouchY        int
	lastTouchAtMS     int64
}

const (
	// Keep only a small quadrant visible, tucked into the lower-left edge.
	intercomCenterX = -24
	intercomCenterY = 506
	intercomRadius  = 114
	micIconLeft     = 8
	micIconTop      = 422
	micIconWidth    = 40
	micIconHeight   = 48
)

// The right-hand content can use the lower half of the display while leaving
// a narrow gutter beside the clock and connection status.
func rightPane(width, height int) image.Rectangle {
	return image.Rect(width*45/100, 24, width-36, height-20)
}

// Crown has enough horizontal room to bring its permanent conditions and
// rotating context panes closer to the clock. Checkers keeps its original
// 45-percent split exactly as shipped.
func crownPane(width, height int) image.Rectangle {
	return image.Rect(width*35/100, 24, width-36, height-20)
}

func musicBarsBounds(pane image.Rectangle) image.Rectangle {
	return image.Rect(pane.Min.X, 348, pane.Max.X, 430)
}

func timerStopBounds(width, height int) image.Rectangle {
	return timerStopBoundsIn(rightPane(width, height))
}

func timerStopBoundsIn(pane image.Rectangle) image.Rectangle {
	cx := (pane.Min.X + pane.Max.X) / 2
	return image.Rect(cx-145, pane.Max.Y-64, cx+145, pane.Max.Y)
}

func intercomGeometry(width, height int) (centerX, centerY, radius, iconLeft, iconTop int) {
	if width <= 0 {
		width = 960
	}
	if height <= 0 {
		height = 480
	}
	iconLeft = micIconLeft
	if width >= 1200 {
		iconLeft -= 4
	}
	return -max(24, width/40), height + 26, intercomRadius, iconLeft, height - 58
}

func checkersMusicBarsBounds(width, height int) image.Rectangle {
	return musicBarsBounds(rightPane(width, height))
}

func (r *renderer) render(canvas *image.RGBA, now time.Time) {
	r.presentRegions = []image.Rectangle{canvas.Rect}
	width, height := canvas.Rect.Dx(), canvas.Rect.Dy()
	accent := accentForTheme(r.state.Phase, r.state.DisplayTheme)
	seconds := now.Sub(r.animationStart).Seconds()
	if r.animationStart.IsZero() {
		seconds = 0
	}
	r.updateMusicLevels(now, seconds)
	if width == height {
		if r.canCacheSpot(now) {
			key := r.spotFrameCacheKey(now, accent)
			rebuilt := r.spotFrameBase == nil || r.spotFrameBase.Rect != canvas.Rect || key != r.spotFrameBaseKey
			if rebuilt {
				r.spotFrameBase = image.NewRGBA(canvas.Rect)
				r.renderSpotFrame(r.spotFrameBase, now, seconds, accent, false)
				r.spotFrameBaseKey = key
			}
			if rebuilt || r.spotFrameCanvas != canvas {
				draw.Draw(canvas, canvas.Rect, r.spotFrameBase, canvas.Rect.Min, draw.Src)
				r.presentRegions = []image.Rectangle{canvas.Rect}
			} else {
				r.presentRegions = r.spotMotionRegions()
				restoreRegions(canvas, r.spotFrameBase, r.presentRegions)
			}
			r.drawSpotMotion(canvas, now, seconds, accent)
			r.spotFrameCanvas = canvas
			return
		}
		r.spotFrameBaseKey = ""
		r.spotFrameCanvas = nil
		r.renderSpot(canvas, now, seconds, accent)
		return
	}
	if r.canCacheCrown() {
		key := r.crownCacheKey(now, accent)
		rebuilt := r.crownBase == nil || r.crownBase.Rect != canvas.Rect || key != r.crownBaseKey
		if rebuilt {
			r.crownBase = image.NewRGBA(canvas.Rect)
			r.renderCheckers(r.crownBase, now, width, height, seconds, accent, false)
			r.crownBaseKey = key
		}
		if rebuilt || r.crownCanvas != canvas {
			draw.Draw(canvas, canvas.Rect, r.crownBase, canvas.Rect.Min, draw.Src)
			r.presentRegions = []image.Rectangle{canvas.Rect}
		} else {
			r.presentRegions = r.crownMotionRegions(now, width, height)
			for _, region := range r.presentRegions {
				region = region.Intersect(canvas.Rect)
				if !region.Empty() {
					draw.Draw(canvas, region, r.crownBase, region.Min, draw.Src)
				}
			}
		}
		r.drawCrownMotion(canvas, now, width, height, seconds, accent)
		r.crownCanvas = canvas
		return
	}
	r.crownBaseKey = ""
	r.crownCanvas = nil
	if r.canCacheCheckers(now) {
		key := r.checkersCacheKey(now, accent)
		rebuilt := r.checkersBase == nil || r.checkersBase.Rect != canvas.Rect || key != r.checkersBaseKey
		if rebuilt {
			r.checkersBase = image.NewRGBA(canvas.Rect)
			r.renderCheckers(r.checkersBase, now, width, height, seconds, accent, false)
			r.checkersBaseKey = key
		}
		if rebuilt || r.checkersCanvas != canvas {
			draw.Draw(canvas, canvas.Rect, r.checkersBase, canvas.Rect.Min, draw.Src)
			r.presentRegions = []image.Rectangle{canvas.Rect}
		} else {
			r.presentRegions = r.checkersMotionRegions(now, width, height)
			restoreRegions(canvas, r.checkersBase, r.presentRegions)
		}
		r.drawCheckersMotion(canvas, now, width, height, seconds, accent)
		r.checkersCanvas = canvas
		return
	}
	r.checkersBaseKey = ""
	r.checkersCanvas = nil
	r.renderCheckers(canvas, now, width, height, seconds, accent, true)
}

func restoreRegions(canvas, base *image.RGBA, regions []image.Rectangle) {
	for _, region := range regions {
		region = region.Intersect(canvas.Rect).Intersect(base.Rect)
		if !region.Empty() {
			draw.Draw(canvas, region, base, region.Min, draw.Src)
		}
	}
}

func (r *renderer) renderCheckers(canvas *image.RGBA, now time.Time, width, height int, seconds float64, accent color.RGBA, motion bool) {
	if r.state.Phase != "speaking" {
		r.checkersReplyGlow = 0
	}
	if r.state.Phase == "setup" {
		r.drawSetup(canvas, seconds, accent)
		return
	}
	if !r.state.Connected && (r.state.Phase == "offline" || r.state.Phase == "connecting") {
		r.drawConnecting(canvas, seconds)
		return
	}
	fillGradient(canvas, color.RGBA{6, 11, 19, 255}, mix(color.RGBA{6, 11, 19, 255}, accent, .13))
	if motion && r.state.Phase == "speaking" {
		r.drawCheckersReplyRim(canvas, accent)
	}

	local := r.localTime(now)
	r.text(canvas, 42, 120, local.Format("3:04"), 96, false, color.White)
	r.text(canvas, 46, 164, local.Format("Monday, January 2"), 22, false, color.RGBA{180, 190, 207, 255})
	room := strings.ToUpper(strings.TrimSpace(r.state.Room))
	if room == "" {
		room = strings.ToUpper(r.showTarget())
	}
	pane := rightPane(width, height)
	if r.isCrown() {
		pane = crownPane(width, height)
	}
	leftWidth := pane.Min.X - 68
	r.text(canvas, 46, 202, fitTextToWidth(room, r.faces.bold[18], leftWidth), 18, true, accent)

	headline, detail := r.status(local)
	r.text(canvas, 42, 286, fitTextToWidth(headline, r.faces.bold[30], leftWidth), 30, true, color.White)
	r.text(canvas, 42, 323, fitTextToWidth(detail, r.faces.regular[20], leftWidth), 20, false, color.RGBA{151, 164, 184, 255})
	statusColor := color.RGBA{255, 158, 72, 255}
	status := "Native service offline"
	if r.state.Connected {
		statusColor = color.RGBA{77, 223, 158, 255}
		status = connectedStatus(r.state.AssistantName)
	}
	circle(canvas, 49, 352, 6, statusColor)
	r.text(canvas, 64, 359, fitTextToWidth(status, r.faces.regular[16], leftWidth-22), 16, false, color.RGBA{150, 163, 181, 255})

	if r.isCrown() {
		r.drawCrownRight(canvas, now, width, height, seconds, accent, motion)
	} else {
		r.drawRightWithMotion(canvas, now, width, height, seconds, accent, motion)
	}
	// The reply rim is composited as the moving layer on cached frames. Leave
	// controls that overlap its outer edge out of the static base so they can
	// be painted once, above the rim, exactly as they are on a full render.
	if motion || r.state.Phase != "speaking" {
		r.drawIntercom(canvas, accent)
		r.drawMutedBadge(canvas, width)
	}
}

func (r *renderer) drawMutedBadge(canvas *image.RGBA, width int) {
	if !r.state.Muted {
		return
	}
	roundedRect(canvas, image.Rect(width-174, 22, width-26, 58), 18, color.RGBA{101, 27, 38, 225})
	r.text(canvas, width-153, 48, "MIC MUTED", 16, true, color.RGBA{255, 191, 197, 255})
}

func (r *renderer) canCacheCheckers(now time.Time) bool {
	if r.isCrown() || r.spotScreen || !r.state.Connected || r.intercomPressed || r.timerPressed || r.notificationActive(now) {
		return false
	}
	if r.state.Timer != nil && r.state.Timer.Active {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(r.state.Phase)) {
	case "", "idle", "playing", "music", "listening", "thinking", "speaking", "intercom", "tool_call":
		return true
	default:
		return false
	}
}

func (r *renderer) canCacheCrown() bool {
	if !r.isCrown() || !r.state.Connected || r.intercomPressed || r.timerPressed {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(r.state.Phase)) {
	case "", "idle", "playing", "music", "listening", "thinking", "speaking", "intercom":
		return true
	default:
		return false
	}
}

func (r *renderer) crownCacheKey(now time.Time, accent color.RGBA) string {
	timerSecond := int64(0)
	if r.state.Timer != nil && r.state.Timer.Active {
		timerSecond = timerRemaining(r.state.Timer, time.UnixMilli(r.currentTaterUnixMS(now))).Milliseconds() / 1000
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%t|%t|%v|%#v|%#v|%#v|%#v|%s|%s|%s|%v|%v|%d|%s|%s|%s",
		r.crownContextKind(now), r.state.Phase, r.state.Message, r.state.DeviceName, r.state.Room,
		r.state.Connected, r.state.Muted, accent, r.state.Weather,
		r.state.Notification, r.state.LastEvent, r.state.Timer,
		mediaIdentity(r.state.Media), mediaPlaybackState(r.state.Media), mediaGroupName(r.state.Media),
		mediaPrimaryColor(r.state.Media), mediaAccentColor(r.state.Media),
		timerSecond,
		r.notificationID, r.lastEventID, r.localTime(now).Format("200601021504"))
}

func mediaPlaybackState(media *show.Media) string {
	if media == nil {
		return ""
	}
	return media.PlaybackState
}

func mediaGroupName(media *show.Media) string {
	if media == nil {
		return ""
	}
	return media.GroupName
}

func mediaPrimaryColor(media *show.Media) [3]uint8 {
	if media == nil {
		return [3]uint8{}
	}
	return media.PrimaryColor
}

func mediaAccentColor(media *show.Media) [3]uint8 {
	if media == nil {
		return [3]uint8{}
	}
	return media.AccentColor
}

func mediaCacheIdentity(media *show.Media) string {
	if media == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%v|%v", mediaIdentity(media), media.PlaybackState,
		media.GroupName, media.AlbumArtist, media.PrimaryColor, media.AccentColor)
}

func (r *renderer) checkersCacheKey(now time.Time, accent color.RGBA) string {
	media := r.state.Media
	local := r.localTime(now)
	return strings.Join([]string{
		r.state.Phase,
		r.state.ToolName,
		r.state.ToolMessage,
		mediaCacheIdentity(media),
		r.state.Room,
		r.state.DisplayTheme,
		r.state.Message,
		r.state.DeviceName,
		r.state.AssistantName,
		fmt.Sprint(r.state.Connected, "|", r.state.Muted, "|", r.state.TimerActive),
		fmt.Sprint(accent),
		fmt.Sprintf("%#v", r.state.Weather),
		fmt.Sprintf("%#v", r.state.Timer),
		local.Format("200601021504"),
		fmt.Sprint(r.mediaImage != nil),
	}, "|")
}

func (r *renderer) drawCheckersMotion(canvas *image.RGBA, now time.Time, width, height int, seconds float64, accent color.RGBA) {
	speaking := r.state.Phase == "speaking"
	if speaking {
		r.drawCheckersReplyRim(canvas, accent)
	}
	if r.state.Timer != nil && r.state.Timer.Active {
		if speaking {
			r.drawIntercom(canvas, accent)
			r.drawMutedBadge(canvas, width)
		}
		return
	}
	if media := r.state.Media; media != nil && media.Active {
		pane := rightPane(width, height)
		musicColor := mediaDisplayColor(media, accent)
		if r.mediaImage == nil {
			r.drawMusicVisualizer(canvas, image.Rect(pane.Min.X, 75, pane.Min.X+202, 277), seconds, musicColor)
		}
		r.drawMediaProgress(canvas, pane, media, now, musicColor)
		r.drawMusicBars(canvas, checkersMusicBarsBounds(width, height), seconds, musicColor)
		if speaking {
			r.drawIntercom(canvas, accent)
			r.drawMutedBadge(canvas, width)
		}
		return
	}
	if weather := r.state.Weather; weather != nil {
		pane := rightPane(width, height)
		kind := weatherDisplayKind(weather, r.localTime(now))
		r.drawWeatherIcon(canvas, pane.Max.X-68, 98, 62, kind, seconds, weatherColor(kind))
		if speaking {
			r.drawIntercom(canvas, accent)
			r.drawMutedBadge(canvas, width)
		}
		return
	}
	if r.state.Phase != "tool_call" && checkersVoiceStageLabel(r.state.Phase) == "" {
		pane := rightPane(width, height)
		r.drawOrb(canvas, (pane.Min.X+pane.Max.X)/2, 220, 120, seconds, accent)
	}
	if speaking {
		r.drawIntercom(canvas, accent)
		r.drawMutedBadge(canvas, width)
	}
}

func (r *renderer) checkersMotionRegions(now time.Time, width, height int) []image.Rectangle {
	regions := make([]image.Rectangle, 0, 5)
	if r.state.Phase == "speaking" {
		regions = append(regions, image.Rect(0, 0, width, 24), image.Rect(0, height-24, width, height),
			image.Rect(0, 24, 24, height-24), image.Rect(width-24, 24, width, height-24),
			image.Rect(0, height-100, 110, height), image.Rect(width-184, 16, width-16, 66))
	}
	if r.state.Timer != nil && r.state.Timer.Active {
		return regions
	}
	pane := rightPane(width, height)
	if media := r.state.Media; media != nil && media.Active {
		if r.mediaImage == nil {
			regions = append(regions, image.Rect(pane.Min.X, 75, pane.Min.X+202, 277))
		}
		regions = append(regions, image.Rect(pane.Min.X, 298, pane.Max.X, 354))
		regions = append(regions, checkersMusicBarsBounds(width, height))
		return regions
	}
	if r.state.Weather != nil {
		cx, cy := pane.Max.X-68, 98
		regions = append(regions, image.Rect(cx-106, cy-106, cx+107, cy+132))
		return regions
	}
	if r.state.Phase != "tool_call" && checkersVoiceStageLabel(r.state.Phase) == "" {
		cx := (pane.Min.X + pane.Max.X) / 2
		regions = append(regions, image.Rect(cx-190, 30, cx+191, 410))
	}
	return regions
}

func (r *renderer) drawSetup(canvas *image.RGBA, seconds float64, accent color.RGBA) {
	fillGradient(canvas, color.RGBA{17, 9, 5, 255}, color.RGBA{8, 13, 18, 255})
	r.text(canvas, 38, 55, "TATER "+strings.ToUpper(r.showTarget())+" SETUP", 18, true, color.RGBA{255, 194, 132, 255})
	r.text(canvas, 38, 105, "Connect this Show to Tater", 36, true, color.White)
	drawSetupCard := func(rect image.Rectangle, number, label, value string) {
		roundedRect(canvas, rect, 14, color.RGBA{25, 28, 34, 230})
		circleOutline(canvas, rect.Min.X+38, (rect.Min.Y+rect.Max.Y)/2, 23, 2, color.RGBA{accent.R, accent.G, accent.B, 180})
		r.centeredText(canvas, rect.Min.X+38, (rect.Min.Y+rect.Max.Y)/2+7, number, 20, true, accent)
		r.text(canvas, rect.Min.X+76, rect.Min.Y+31, label, 14, true, color.RGBA{160, 172, 190, 255})
		r.text(canvas, rect.Min.X+76, rect.Min.Y+62, ellipsize(value, 28), 20, true, color.White)
	}
	if strings.HasPrefix(r.state.Room, "Tater-Setup-") {
		drawSetupCard(image.Rect(38, 136, 465, 222), "1", "JOIN WI-FI", r.state.Room)
		drawSetupCard(image.Rect(495, 136, 922, 222), "2", "OPEN IN BROWSER", "192.168.4.1")
		r.text(canvas, 38, 282, "Join the open Tater setup network on your phone.", 24, true, color.White)
		r.text(canvas, 38, 321, "In Tater, use Satellites  ›  Add Satellite for a pairing code.", 18, false, color.RGBA{180, 188, 199, 255})
	} else {
		drawSetupCard(image.Rect(38, 136, 465, 222), "1", "CONNECT USB", "Open the Tater desktop app")
		drawSetupCard(image.Rect(495, 136, 922, 222), "2", "PROVISION", "Satellites  ›  Add Satellite")
		r.text(canvas, 38, 282, "Keep USB connected while Wi-Fi and pairing are saved.", 24, true, color.White)
		r.text(canvas, 38, 321, "The screen will continue automatically after provisioning.", 20, false, color.RGBA{180, 188, 199, 255})
	}
	for index := 0; index < 3; index++ {
		alpha := uint8(70)
		if int(seconds*2)%3 == index {
			alpha = 255
		}
		circle(canvas, 48+index*24, 374, 6, color.RGBA{accent.R, accent.G, accent.B, alpha})
	}
	if strings.HasPrefix(r.state.Room, "Tater-Setup-") {
		r.text(canvas, 126, 381, "Waiting for Wi-Fi and Tater pairing", 18, true, color.RGBA{200, 209, 222, 255})
		r.text(canvas, 38, 449, "USB provisioning is also available if needed.", 16, false, color.RGBA{126, 136, 150, 255})
	} else {
		r.text(canvas, 126, 381, "Waiting for secure USB provisioning", 18, true, color.RGBA{200, 209, 222, 255})
		r.text(canvas, 38, 449, "The setup hotspot could not start. USB remains available.", 16, false, color.RGBA{126, 136, 150, 255})
	}
}

func (r *renderer) localTime(now time.Time) time.Time {
	if r.state.TaterTimeUnixMS <= 0 {
		return now
	}
	base := time.UnixMilli(r.state.TaterTimeUnixMS)
	if !r.received.IsZero() {
		base = base.Add(now.Sub(r.received))
	}
	return base.UTC().Add(time.Duration(r.state.TaterUTCOffset) * time.Second)
}

func (r *renderer) showTarget() string {
	if r.isCrown() {
		return "Crown"
	}
	return "Checkers"
}

func (r *renderer) isCrown() bool {
	return strings.EqualFold(strings.TrimSpace(r.target), "crown")
}

func (r *renderer) contentPane(width, height int) image.Rectangle {
	if r.isCrown() {
		return crownPane(width, height)
	}
	return rightPane(width, height)
}

func (r *renderer) showSize() (int, int) {
	width, height := r.screenWidth, r.screenHeight
	if width <= 0 {
		width = 960
	}
	if height <= 0 {
		height = 480
	}
	return width, height
}

func (r *renderer) status(local time.Time) (string, string) {
	if r.state.Phase == "tool_call" {
		tool := titleWords(r.state.ToolName)
		if tool == "" {
			tool = "Tater"
		}
		return "Using " + tool, firstNonEmpty(r.state.ToolMessage, r.state.Message, "Working on it")
	}
	message := firstNonEmpty(r.state.Message, "Ready when you are")
	if r.state.Connected && r.state.Phase == "idle" && message == "Ready when you are" {
		hour := local.Hour()
		switch {
		case hour >= 5 && hour < 12:
			message = "Good morning"
		case hour >= 12 && hour < 17:
			message = "Good afternoon"
		case hour >= 17 && hour < 21:
			message = "Good evening"
		default:
			message = "Good night"
		}
	}
	return message, firstNonEmpty(r.state.DeviceName, "Tater Checkers")
}

func (r *renderer) drawRight(canvas *image.RGBA, now time.Time, width, height int, seconds float64, accent color.RGBA) {
	r.drawRightWithMotion(canvas, now, width, height, seconds, accent, true)
}

func (r *renderer) drawRightWithMotion(canvas *image.RGBA, now time.Time, width, height int, seconds float64, accent color.RGBA, motion bool) {
	if r.notificationActive(now) {
		r.drawNotification(canvas, now, width, seconds, accent)
		return
	}
	if r.state.Timer != nil && r.state.Timer.Active {
		r.drawTimer(canvas, now, width, seconds, accent)
		return
	}
	if r.state.Media != nil && r.state.Media.Active {
		r.drawNowPlaying(canvas, now, width, seconds, accent, motion)
		return
	}
	if r.state.Weather != nil {
		r.drawWeather(canvas, now, width, seconds, accent, motion)
		return
	}
	if r.state.Phase == "tool_call" {
		pane := rightPane(width, height)
		cx := (pane.Min.X + pane.Max.X) / 2
		r.centeredText(canvas, cx, 220, "WORKING", 18, true, color.White)
		r.centeredText(canvas, cx, 257, ellipsize(titleWords(r.state.ToolName), 23), 20, false, color.RGBA{180, 190, 207, 255})
		return
	}
	pane := rightPane(width, height)
	if label := checkersVoiceStageLabel(r.state.Phase); label != "" {
		r.centeredText(canvas, (pane.Min.X+pane.Max.X)/2, 228, label, 20, true, accent)
		return
	}
	if motion {
		r.drawOrb(canvas, (pane.Min.X+pane.Max.X)/2, 220, 120, seconds, accent)
	}
}

const crownContextHeight = 480

func (r *renderer) drawCrownRight(canvas *image.RGBA, now time.Time, width, height int, seconds float64, accent color.RGBA, motion bool) {
	r.drawCrownConditions(canvas, now, width, seconds, accent, motion)

	// Reuse the original 480-pixel-tall Checkers content geometry below the
	// permanent conditions region. This keeps the mature music, timer, and
	// Awareness layouts intact while using Crown's extra vertical space.
	contextBounds := image.Rect(0, 0, width, crownContextHeight)
	if r.crownContext == nil || r.crownContext.Rect != contextBounds {
		r.crownContext = image.NewRGBA(contextBounds)
	} else {
		draw.Draw(r.crownContext, contextBounds, image.Transparent, image.Point{}, draw.Src)
	}
	r.drawCrownContext(r.crownContext, now, width, seconds, accent, motion)
	offsetY := max(0, height-crownContextHeight)
	source := crownPane(width, crownContextHeight)
	destination := source.Add(image.Pt(0, offsetY))
	draw.Draw(canvas, destination, r.crownContext, source.Min, draw.Over)
}

func (r *renderer) drawCrownConditions(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA, motion bool) {
	pane := crownPane(width, canvas.Rect.Dy())
	left, right := pane.Min.X, pane.Max.X
	weather := r.state.Weather
	weatherAccent := accent
	conditionKind := ""
	if weather != nil {
		conditionKind = weatherDisplayKind(weather, r.localTime(now))
		weatherAccent = weatherColor(conditionKind)
	}
	r.text(canvas, left, 52, "OUTSIDE CONDITIONS", 16, true, accent)
	if weather != nil && weather.Stale {
		staleWidth := font.MeasureString(r.faces.bold[14], "STALE").Round()
		r.text(canvas, right-staleWidth, 52, "STALE", 14, true, color.RGBA{255, 170, 92, 255})
	}

	temperature, condition, feels := "--°", "Weather unavailable", ""
	if weather != nil {
		temperature = firstNonEmpty(weather.TemperatureText, "--°")
		condition = firstNonEmpty(weather.Condition, "Conditions unavailable")
		feels = weather.FeelsLikeText
	}
	if motion {
		r.drawWeatherIcon(canvas, right-68, 112, 56, conditionKind, seconds, weatherAccent)
	}
	temperature = ellipsize(temperature, 7)
	r.text(canvas, left, 148, temperature, 74, true, color.White)
	temperatureWidth := font.MeasureString(r.faces.bold[74], temperature).Round()
	if weather != nil && weather.TemperatureUnit != "" {
		r.text(canvas, left+temperatureWidth+8, 116, weather.TemperatureUnit, 18, true, color.RGBA{174, 189, 209, 255})
	}
	if feels != "" {
		feelsInk := color.RGBA{174, 189, 209, 255}
		if weather.FeelsLikeRelation == "cooler" {
			feelsInk = color.RGBA{83, 178, 255, 255}
		} else if weather.FeelsLikeRelation == "warmer" {
			feelsInk = color.RGBA{255, 147, 66, 255}
		}
		feelsLeft := min(left+max(temperatureWidth+28, 190), right-250)
		r.text(canvas, feelsLeft, 108, "FEELS LIKE", 14, true, feelsInk)
		r.text(canvas, feelsLeft, 139, ellipsize(trimFeelsLike(feels), 10), 24, true, feelsInk)
	}
	r.text(canvas, left, 194, fitTextToWidth(condition, r.faces.bold[24], pane.Dx()-90), 24, true, color.RGBA{235, 240, 248, 255})

	type metric struct {
		kind, label, value string
		ink                color.RGBA
	}
	metrics := []metric{}
	if weather != nil {
		if weather.HumidityText != "" {
			metrics = append(metrics, metric{"humidity", "HUMIDITY", weather.HumidityText, color.RGBA{83, 178, 255, 255}})
		}
		if weather.WindText != "" {
			metrics = append(metrics, metric{"wind", "WIND", weather.WindText, color.RGBA{143, 211, 222, 255}})
		}
		if weather.RainText != "" {
			metrics = append(metrics, metric{"rain", "RAIN", weather.RainText, color.RGBA{108, 162, 255, 255}})
		}
		if weather.LightningText != "" {
			metrics = append(metrics, metric{"lightning", "LIGHTNING", weather.LightningText, color.RGBA{255, 208, 92, 255}})
		}
		if weather.IndoorTemperatureText != "" {
			metrics = append(metrics, metric{"temperature", "ROOM TEMP", weather.IndoorTemperatureText, color.RGBA{255, 147, 66, 255}})
		}
		if weather.IndoorHumidityText != "" {
			metrics = append(metrics, metric{"humidity", "ROOM HUMIDITY", weather.IndoorHumidityText, color.RGBA{83, 178, 255, 255}})
		}
	}
	shown := metrics[:min(6, len(metrics))]
	for index, item := range shown {
		x := left + pane.Dx()*(index*2+1)/(max(1, len(shown))*2)
		r.drawWeatherMetricGlyph(canvas, x, 240, 11, item.kind, item.ink)
		r.centeredText(canvas, x, 273, fitTextToWidth(item.value, r.faces.bold[18], max(80, pane.Dx()/max(1, len(shown))-8)), 18, true, color.RGBA{238, 243, 250, 255})
		r.centeredText(canvas, x, 296, item.label, 12, true, item.ink)
	}
	line(canvas, left, 310, right, 310, 1, color.RGBA{accent.R, accent.G, accent.B, 50})
}
func (r *renderer) crownContextKind(now time.Time) string {
	if r.notificationActive(now) {
		return "notification"
	}
	if r.state.Timer != nil && r.state.Timer.Active {
		return "timer"
	}
	if r.state.Media != nil && r.state.Media.Active {
		return "media"
	}
	if r.state.Phase == "tool_call" {
		return "tool"
	}
	if r.state.LastEvent != nil {
		return "last_event"
	}
	return "empty"
}

func (r *renderer) drawCrownContext(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA, motion bool) {
	pane := crownPane(width, crownContextHeight)
	switch r.crownContextKind(now) {
	case "notification":
		r.drawCrownEventCard(canvas, now, width, seconds, accent, r.state.Notification, r.notificationImage, true)
	case "timer":
		r.drawTimer(canvas, now, width, seconds, accent)
	case "media":
		r.drawNowPlaying(canvas, now, width, seconds, accent, motion)
	case "tool":
		cx := (pane.Min.X + pane.Max.X) / 2
		r.centeredText(canvas, cx, 220, "WORKING", 18, true, color.White)
		r.centeredText(canvas, cx, 257, ellipsize(titleWords(r.state.ToolName), 23), 20, false, color.RGBA{180, 190, 207, 255})
	case "last_event":
		r.drawLastEvent(canvas, now, width, seconds, accent)
	case "empty":
		r.text(canvas, pane.Min.X, 52, "LAST EVENT", 16, true, accent)
		r.centeredText(canvas, (pane.Min.X+pane.Max.X)/2, 228, "NO LAST EVENT", 20, true, color.RGBA{151, 164, 184, 255})
	default:
		r.drawOrb(canvas, (pane.Min.X+pane.Max.X)/2, 220, 120, seconds, accent)
	}
}

func (r *renderer) drawCrownMotion(canvas *image.RGBA, now time.Time, width, height int, seconds float64, accent color.RGBA) {
	speaking := r.state.Phase == "speaking"
	if speaking {
		r.drawCheckersReplyRim(canvas, accent)
		defer func() {
			r.drawIntercom(canvas, accent)
			r.drawMutedBadge(canvas, width)
		}()
	}
	if weather := r.state.Weather; weather != nil {
		pane := crownPane(width, height)
		kind := weatherDisplayKind(weather, r.localTime(now))
		r.drawWeatherIcon(canvas, pane.Max.X-68, 112, 56, kind, seconds, weatherColor(kind))
	}
	if r.crownContextKind(now) != "media" || r.state.Media == nil {
		return
	}
	offsetY := max(0, height-crownContextHeight)
	pane := crownPane(width, crownContextHeight)
	musicColor := mediaDisplayColor(r.state.Media, accent)
	r.drawMediaProgress(canvas, pane.Add(image.Pt(0, offsetY)), r.state.Media, now, musicColor)
	if r.mediaImage == nil {
		art := image.Rect(pane.Min.X, 75, pane.Min.X+202, 277).Add(image.Pt(0, offsetY))
		r.drawMusicVisualizer(canvas, art, seconds, musicColor)
	}
	bars := musicBarsBounds(pane).Add(image.Pt(0, offsetY))
	r.drawMusicBars(canvas, bars, seconds, musicColor)
}

func (r *renderer) crownMotionRegions(now time.Time, width, height int) []image.Rectangle {
	regions := make([]image.Rectangle, 0, 10)
	if r.state.Phase == "speaking" {
		regions = append(regions,
			image.Rect(0, 0, width, 24), image.Rect(0, height-24, width, height),
			image.Rect(0, 24, 24, height-24), image.Rect(width-24, 24, width, height-24),
			image.Rect(0, height-100, 110, height), image.Rect(width-184, 16, width-16, 66),
		)
	}
	if r.state.Weather != nil {
		pane := crownPane(width, height)
		cx, cy := pane.Max.X-68, 112
		regions = append(regions, image.Rect(cx-96, cy-96, cx+97, cy+97))
	}
	if r.crownContextKind(now) != "media" || r.state.Media == nil {
		return regions
	}
	offset := image.Pt(0, max(0, height-crownContextHeight))
	pane := crownPane(width, crownContextHeight)
	if r.mediaImage == nil {
		regions = append(regions, image.Rect(pane.Min.X, 75, pane.Min.X+202, 277).Add(offset))
	}
	regions = append(regions, image.Rect(pane.Min.X, 298, pane.Max.X, 354).Add(offset))
	regions = append(regions, musicBarsBounds(pane).Add(offset))
	return regions
}

func (r *renderer) drawNowPlaying(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA, musicMotion bool) {
	media := r.state.Media
	pane := r.contentPane(width, canvas.Rect.Dy())
	left, right := pane.Min.X, pane.Max.X
	musicColor := mediaDisplayColor(media, accent)
	heading := r.nowPlayingHeading(media)
	r.text(canvas, left, 52, fitTextToWidth(heading, r.faces.bold[16], pane.Dx()), 16, true, musicColor)

	art := image.Rect(left, 75, left+202, 277)
	roundedRect(canvas, art, 20, mix(color.RGBA{16, 23, 35, 255}, musicColor, .13))
	if r.mediaImage != nil {
		drawCover(canvas, art, r.mediaImage)
	} else if musicMotion {
		r.drawMusicVisualizer(canvas, art, seconds, musicColor)
	}

	textLeft := art.Max.X + 26
	textWidth := max(1, right-textLeft)
	titleLines := wrapTextToWidth(firstNonEmpty(media.Title, "Music"), r.faces.bold[30], textWidth)
	for index, value := range titleLines {
		if index >= 3 {
			break
		}
		r.text(canvas, textLeft, 116+index*35, fitTextToWidth(value, r.faces.bold[30], textWidth), 30, true, color.White)
	}
	artistBaseline := 229
	if len(titleLines) < 3 {
		artistBaseline = 123 + len(titleLines)*35
	}
	r.text(canvas, textLeft, artistBaseline, fitTextToWidth(firstNonEmpty(media.Artist, media.AlbumArtist, "Tater Music"), r.faces.bold[18], textWidth), 18, true, color.RGBA{216, 225, 238, 255})
	if album := strings.TrimSpace(media.Album); album != "" {
		r.text(canvas, textLeft, artistBaseline+27, fitTextToWidth(album, r.faces.regular[16], textWidth), 16, false, color.RGBA{146, 162, 185, 255})
	}

	if musicMotion {
		r.drawMediaProgress(canvas, pane, media, now, musicColor)
	}
	if musicMotion {
		r.drawMusicBars(canvas, musicBarsBounds(pane), seconds, musicColor)
	}
}

func (r *renderer) drawMediaProgress(canvas *image.RGBA, pane image.Rectangle, media *show.Media, now time.Time, musicColor color.RGBA) {
	progress, duration := mediaProgress(media, now)
	if duration <= 0 {
		return
	}
	left, right := pane.Min.X, pane.Max.X
	barY := pane.Min.Y + 292
	line(canvas, left, barY, right, barY, 4, color.RGBA{91, 103, 122, 95})
	filled := int(float64(pane.Dx()) * math.Min(1, float64(progress)/float64(duration)))
	line(canvas, left, barY, left+filled, barY, 4, musicColor)
	r.text(canvas, left, barY+28, formatMediaTime(progress), 14, true, color.RGBA{192, 205, 222, 255})
	end := formatMediaTime(duration)
	endWidth := font.MeasureString(r.faces.bold[14], end).Round()
	r.text(canvas, right-endWidth, barY+28, end, 14, true, color.RGBA{141, 156, 178, 255})
}

func nowPlayingHeading(state show.Snapshot, media *show.Media) string {
	switch state.Phase {
	case "listening":
		return "LISTENING"
	case "thinking":
		return "THINKING"
	case "speaking":
		return "SPEAKING"
	case "intercom":
		return "INTERCOM"
	case "tool_call":
		if tool := titleWords(state.ToolName); tool != "" {
			return "USING " + strings.ToUpper(tool)
		}
		return "WORKING"
	}
	heading := "NOW PLAYING"
	if media != nil && strings.EqualFold(media.PlaybackState, "paused") {
		heading = "PAUSED"
	}
	if media != nil {
		if group := strings.TrimSpace(media.GroupName); group != "" {
			heading += "  ·  " + strings.ToUpper(group)
		}
	}
	return heading
}

func (r *renderer) nowPlayingHeading(media *show.Media) string {
	state := r.state
	if r.isCrown() && checkersVoiceStageLabel(state.Phase) != "" {
		// Crown already reports the voice stage in its permanent left column.
		// Keep the lower context card focused on the active media instead of
		// duplicating LISTENING/THINKING/SPEAKING/INTERCOM over it.
		state.Phase = "idle"
	}
	return nowPlayingHeading(state, media)
}

func (r *renderer) drawMusicVisualizer(canvas *image.RGBA, bounds image.Rectangle, seconds float64, ink color.RGBA) {
	cx, cy := (bounds.Min.X+bounds.Max.X)/2, (bounds.Min.Y+bounds.Max.Y)/2
	circle(canvas, cx, cy, min(bounds.Dx(), bounds.Dy())/3, color.RGBA{ink.R, ink.G, ink.B, 30})
	for index := 0; index < 5; index++ {
		height := int((.18 + .52*r.musicBinLevel(index, seconds)) * float64(bounds.Dy()))
		x := cx - 42 + index*21
		roundedRect(canvas, image.Rect(x-5, cy-height/2, x+6, cy+height/2), 5, color.RGBA{ink.R, ink.G, ink.B, 210})
	}
}

func (r *renderer) drawMusicBars(canvas *image.RGBA, bounds image.Rectangle, seconds float64, ink color.RGBA) {
	const count = 12
	gap := 7
	barWidth := max(3, (bounds.Dx()-gap*(count-1))/count)
	for index := 0; index < count; index++ {
		level := r.musicBinLevel(index, seconds)
		// Sendspin spectrum bins intentionally retain headroom. Expand that
		// useful range for the display so normal music travels farther without
		// clipping quiet passages into a permanently tall baseline.
		level = math.Min(1, .04+(level-.04)*1.30)
		height := max(3, int(level*float64(bounds.Dy())))
		x := bounds.Min.X + index*(barWidth+gap)
		roundedRect(canvas, image.Rect(x, bounds.Max.Y-height, x+barWidth, bounds.Max.Y), barWidth/2,
			color.RGBA{ink.R, ink.G, ink.B, uint8(85 + 170*level)})
	}
}

func (r *renderer) updateMusicLevels(now time.Time, seconds float64) {
	media := r.state.Media
	if media == nil || !media.Active || len(media.Spectrum) == 0 {
		r.musicLevelsAt = time.Time{}
		r.musicLevelsMedia = ""
		r.musicLevelsReady = false
		return
	}

	identity := mediaIdentity(media)
	if !r.musicLevelsReady || r.musicLevelsAt.IsZero() || identity != r.musicLevelsMedia || now.Before(r.musicLevelsAt) {
		for index := range r.musicLevels {
			r.musicLevels[index] = musicBinLevel(media, index, seconds, r.state.AudioLevel)
		}
		r.musicLevelsAt = now
		r.musicLevelsMedia = identity
		r.musicLevelsReady = true
		return
	}

	delta := now.Sub(r.musicLevelsAt)
	if delta <= 0 {
		return
	}
	// A fast attack keeps beats feeling immediate; the gentler release removes
	// the jagged stepping between incoming Sendspin spectrum frames.
	for index := range r.musicLevels {
		target := musicBinLevel(media, index, seconds, r.state.AudioLevel)
		tau := 70 * time.Millisecond
		if target > r.musicLevels[index] {
			tau = 22 * time.Millisecond
		}
		alpha := 1 - math.Exp(-float64(delta)/float64(tau))
		r.musicLevels[index] += (target - r.musicLevels[index]) * alpha
	}
	r.musicLevelsAt = now
}

func (r *renderer) musicBinLevel(index int, seconds float64) float64 {
	if r.musicLevelsReady && r.state.Media != nil && len(r.state.Media.Spectrum) > 0 {
		return r.musicLevels[index%len(r.musicLevels)]
	}
	return musicBinLevel(r.state.Media, index, seconds, r.state.AudioLevel)
}

func musicBinLevel(media *show.Media, index int, seconds, fallback float64) float64 {
	if media != nil && len(media.Spectrum) > 0 {
		value := media.Spectrum[index%len(media.Spectrum)]
		return math.Max(.04, math.Min(1, value))
	}
	level := math.Min(1, math.Max(.03, fallback*5))
	wave := .48 + .52*math.Sin(seconds*3.4+float64(index)*.82)
	return math.Max(.04, level*(.32+.68*wave))
}

func mediaDisplayColor(media *show.Media, fallback color.RGBA) color.RGBA {
	if media == nil {
		return fallback
	}
	selected := media.PrimaryColor
	if brighterRGB(media.AccentColor, selected) {
		selected = media.AccentColor
	}
	if selected == ([3]uint8{}) {
		return fallback
	}
	peak := max(selected[0], max(selected[1], selected[2]))
	if peak < 150 {
		scale := 150 / float64(max(peak, 1))
		for index := range selected {
			selected[index] = uint8(math.Min(255, math.Round(float64(selected[index])*scale)))
		}
	}
	return color.RGBA{selected[0], selected[1], selected[2], 255}
}

func brighterRGB(a, b [3]uint8) bool {
	brightness := func(value [3]uint8) int { return int(value[0])*299 + int(value[1])*587 + int(value[2])*114 }
	return brightness(a) > brightness(b)
}

func mediaProgress(media *show.Media, now time.Time) (int64, int64) {
	if media == nil {
		return 0, 0
	}
	progress := media.ProgressMS
	if media.PlaybackSpeed > 0 && media.ProgressUpdatedAtUnixMS > 0 {
		elapsed := max(int64(0), now.UnixMilli()-media.ProgressUpdatedAtUnixMS)
		progress += elapsed * int64(media.PlaybackSpeed) / 1000
	}
	if media.DurationMS > 0 {
		progress = min(progress, media.DurationMS)
	}
	return max(int64(0), progress), max(int64(0), media.DurationMS)
}

func formatMediaTime(milliseconds int64) string {
	seconds := max(int64(0), milliseconds/1000)
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func checkersVoiceStageLabel(phase string) string {
	switch phase {
	case "listening":
		return "LISTENING"
	case "thinking":
		return "THINKING"
	case "speaking":
		return "SPEAKING"
	case "intercom":
		return "INTERCOM"
	default:
		return ""
	}
}

// Match the Spot's audio-following rim on Checkers' rectangular display.
// Painting only the outer 24 pixels keeps the content readable and the per-
// frame work bounded on the Show 5's small CPU.
func (r *renderer) drawCheckersReplyRim(canvas *image.RGBA, replyColor color.RGBA) {
	target := math.Pow(math.Min(1, math.Max(0, r.state.AudioLevel)/.22), .65)
	response := .30
	if target > r.checkersReplyGlow {
		response = .60
	}
	r.checkersReplyGlow += (target - r.checkersReplyGlow) * response
	level := r.checkersReplyGlow
	width, height := canvas.Rect.Dx(), canvas.Rect.Dy()
	ink := mix(replyColor, color.RGBA{255, 255, 255, 255}, .04+.10*level)
	for inset := 0; inset < 24 && inset*2 < width && inset*2 < height; inset++ {
		fade := 1 - float64(inset)/24
		ink.A = uint8((12 + 220*level) * fade * fade)
		if ink.A == 0 {
			continue
		}
		left, right := inset, width-inset-1
		top, bottom := inset, height-inset-1
		for x := left; x <= right; x++ {
			blendPixel(canvas, x, top, ink)
			blendPixel(canvas, x, bottom, ink)
		}
		for y := top + 1; y < bottom; y++ {
			blendPixel(canvas, left, y, ink)
			blendPixel(canvas, right, y, ink)
		}
	}
}

func connectedStatus(name string) string {
	return firstNonEmpty(strings.TrimSpace(name), "Tater") + " Connected"
}

func (r *renderer) drawOrb(canvas *image.RGBA, cx, cy, radius int, seconds float64, accent color.RGBA) {
	audio := math.Min(1, math.Max(0, r.state.AudioLevel*1.8))
	breath := 1 + .035*math.Sin(seconds*math.Pi*1.25)
	activeRadius := int(float64(radius) * breath * (1 + .10*audio))
	circle(canvas, cx, cy, int(float64(activeRadius)*1.30), color.RGBA{accent.R, accent.G, accent.B, 24})
	circle(canvas, cx, cy, int(float64(activeRadius)*1.12), color.RGBA{accent.R, accent.G, accent.B, 50})
	circle(canvas, cx, cy, int(float64(activeRadius)*.76), mix(accent, color.RGBA{15, 20, 31, 255}, .34))
	label := phaseLabel(r.state.Phase)
	if r.state.Muted {
		label = "MUTED"
	}
	r.centeredText(canvas, cx, cy+7, label, 18, true, color.White)
	if r.state.TimerActive {
		circle(canvas, cx+radius*3/4, cy-radius*7/10, 11, color.RGBA{255, 208, 92, 255})
	}
}

func (r *renderer) drawWeather(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA, motion bool) {
	weather := r.state.Weather
	pane := r.contentPane(width, canvas.Rect.Dy())
	left, right := pane.Min.X, pane.Max.X
	displayKind := weatherDisplayKind(weather, r.localTime(now))
	weatherAccent := weatherColor(displayKind)
	heading := "OUTSIDE CONDITIONS"
	if r.state.Phase == "tool_call" {
		heading = "WORKING"
		if tool := titleWords(r.state.ToolName); tool != "" {
			heading = "USING " + strings.ToUpper(tool)
		}
	}
	r.text(canvas, left, 52, fitTextToWidth(heading, r.faces.bold[16], right-left-64), 16, true, accent)
	if motion {
		r.drawWeatherIcon(canvas, right-68, 98, 62, displayKind, seconds, weatherAccent)
	}
	temperature := ellipsize(firstNonEmpty(weather.TemperatureText, "--°"), 6)
	r.text(canvas, left, 154, temperature, 82, true, color.White)
	temperatureWidth := font.MeasureString(r.faces.bold[82], temperature).Round()
	if weather.TemperatureUnit != "" {
		r.text(canvas, left+temperatureWidth+8, 123, weather.TemperatureUnit, 18, true, color.RGBA{174, 189, 209, 255})
	}
	if weather.FeelsLikeText != "" {
		feelsColor := color.RGBA{255, 255, 255, 255}
		if weather.FeelsLikeRelation == "cooler" {
			feelsColor = color.RGBA{83, 178, 255, 255}
		} else if weather.FeelsLikeRelation == "warmer" {
			feelsColor = color.RGBA{255, 147, 66, 255}
		}
		feelsLeft := min(left+max(temperatureWidth+28, 200), right-250)
		r.text(canvas, feelsLeft, 111, "FEELS LIKE", 14, true, feelsColor)
		r.text(canvas, feelsLeft, 145, ellipsize(trimFeelsLike(weather.FeelsLikeText), 10), 24, true, feelsColor)
	}
	r.text(canvas, left, 207, fitTextToWidth(weather.Condition, r.faces.bold[24], right-left-70), 24, true, color.RGBA{235, 240, 248, 255})
	type metric struct {
		kind, label, value string
		ink                color.RGBA
	}
	metrics := []metric{}
	if weather.HumidityText != "" {
		metrics = append(metrics, metric{"humidity", "HUMIDITY", weather.HumidityText, color.RGBA{83, 178, 255, 255}})
	}
	if weather.WindText != "" {
		metrics = append(metrics, metric{"wind", "WIND", weather.WindText, color.RGBA{143, 211, 222, 255}})
	}
	if weather.RainText != "" {
		metrics = append(metrics, metric{"rain", "RAIN", weather.RainText, color.RGBA{108, 162, 255, 255}})
	}
	if weather.LightningText != "" {
		metrics = append(metrics, metric{"lightning", "LIGHTNING", weather.LightningText, color.RGBA{255, 208, 92, 255}})
	}
	for index, item := range metrics {
		x := left + (right-left)*(index*2+1)/(max(1, len(metrics))*2)
		r.drawWeatherMetricGlyph(canvas, x, 272, 13, item.kind, item.ink)
		r.centeredText(canvas, x, 312, ellipsize(item.value, 11), 20, true, color.RGBA{238, 243, 250, 255})
		r.centeredText(canvas, x, 334, item.label, 14, true, item.ink)
	}
	if weather.IndoorTemperatureText != "" || weather.IndoorHumidityText != "" {
		r.drawIndoorClimateStrip(canvas, pane, accent, weather.IndoorTemperatureText, weather.IndoorHumidityText)
	}
	if weather.Stale {
		r.text(canvas, right-48, 52, "STALE", 14, true, color.RGBA{255, 170, 92, 255})
	}
}

func (r *renderer) drawIndoorClimateStrip(canvas *image.RGBA, pane image.Rectangle, accent color.RGBA, temperature, humidity string) {
	left, right := pane.Min.X, pane.Max.X
	titleY, metricY := pane.Max.Y-88, pane.Max.Y-39
	room := strings.TrimSpace(r.state.Room)
	if room == "" || strings.EqualFold(room, "unassigned room") || strings.EqualFold(room, "echo show 5") {
		room = "INDOOR"
	}
	title := ellipsize(strings.ToUpper(room)+" CLIMATE", 24)
	circle(canvas, left+11, titleY-5, 13, color.RGBA{accent.R, accent.G, accent.B, 35})
	line(canvas, left+4, titleY-5, left+11, titleY-11, 2, accent)
	line(canvas, left+11, titleY-11, left+18, titleY-5, 2, accent)
	line(canvas, left+6, titleY-6, left+6, titleY+2, 2, accent)
	line(canvas, left+6, titleY+2, left+16, titleY+2, 2, accent)
	line(canvas, left+16, titleY+2, left+16, titleY-6, 2, accent)
	r.text(canvas, left+28, titleY, title, 16, true, accent)
	lineStart := left + 38 + font.MeasureString(r.faces.bold[16], title).Round()
	if lineStart < right {
		line(canvas, lineStart, titleY-5, right, titleY-5, 1, color.RGBA{accent.R, accent.G, accent.B, 45})
	}
	if temperature != "" && humidity != "" {
		r.drawRoomMetricInline(canvas, left+125, metricY, "temperature", "TEMPERATURE", temperature, color.RGBA{255, 147, 66, 255})
		r.drawRoomMetricInline(canvas, left+330, metricY, "humidity", "HUMIDITY", humidity, color.RGBA{83, 178, 255, 255})
	} else if temperature != "" {
		r.drawRoomMetricInline(canvas, left+pane.Dx()/2, metricY, "temperature", "TEMPERATURE", temperature, color.RGBA{255, 147, 66, 255})
	} else {
		r.drawRoomMetricInline(canvas, left+pane.Dx()/2, metricY, "humidity", "HUMIDITY", humidity, color.RGBA{83, 178, 255, 255})
	}
}

func (r *renderer) drawRoomMetricInline(canvas *image.RGBA, x, y int, kind, label, value string, ink color.RGBA) {
	r.drawWeatherMetricGlyph(canvas, x, y, 13, kind, ink)
	r.text(canvas, x+23, y-4, label, 14, true, ink)
	r.text(canvas, x+23, y+18, ellipsize(value, 10), 20, true, color.RGBA{238, 243, 250, 255})
}

func (r *renderer) drawWeatherMetricGlyph(canvas *image.RGBA, x, y, radius int, kind string, ink color.RGBA) {
	circle(canvas, x, y, radius+7, color.RGBA{ink.R, ink.G, ink.B, 28})
	switch kind {
	case "humidity":
		for row := -8; row <= 8; row++ {
			span := int(float64(radius) * .72 * (1 - math.Abs(float64(row))/15))
			if span > 0 {
				line(canvas, x-span, y+row, x+span, y+row, 1, ink)
			}
		}
	case "wind":
		line(canvas, x-10, y-4, x+7, y-4, 2, ink)
		arc(canvas, x+7, y-7, 3, 90, 240, 2, ink)
		line(canvas, x-10, y+4, x+3, y+4, 2, ink)
		arc(canvas, x+3, y+7, 3, 270, 240, 2, ink)
	case "rain":
		for offset := -1; offset <= 1; offset++ {
			line(canvas, x+offset*6+2, y-7, x+offset*6-2, y+7, 2, ink)
		}
	case "temperature":
		roundedRectOutline(canvas, image.Rect(x-3, y-11, x+4, y+7), 3, 2, ink)
		line(canvas, x, y-6, x, y+7, 2, ink)
		circle(canvas, x, y+8, 4, ink)
	default:
		line(canvas, x+2, y-10, x-5, y+1, 2, ink)
		line(canvas, x-5, y+1, x+1, y, 2, ink)
		line(canvas, x+1, y, x-2, y+10, 2, ink)
		line(canvas, x-2, y+10, x+7, y-2, 2, ink)
	}
}

func (r *renderer) drawTimer(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA) {
	timer := r.state.Timer
	timerNow := time.UnixMilli(r.currentTaterUnixMS(now))
	remaining := timerRemaining(timer, timerNow)
	pane := r.contentPane(width, canvas.Rect.Dy())
	left, right := pane.Min.X, pane.Max.X
	cx, cy, radius := (left+right)/2, 202, 112
	timerAccent := accent
	if timer.Ringing {
		timerAccent = color.RGBA{255, 105, 76, 255}
	}
	heading := "TATER TIMER"
	if timer.Count > 1 {
		heading = fmt.Sprintf("TATER TIMERS  ·  %d", timer.Count)
	}
	r.text(canvas, left, 50, heading, 16, true, timerAccent)
	circleOutline(canvas, cx, cy, radius, 7, color.RGBA{timerAccent.R, timerAccent.G, timerAccent.B, 45})
	if timer.Ringing {
		pulse := .5 + .5*math.Sin(seconds*4.8)
		arc(canvas, cx, cy, radius, -90+seconds*70, 72+pulse*38, 7, timerAccent)
		arc(canvas, cx, cy, radius, 90+seconds*70, 72+pulse*38, 7, timerAccent)
		r.centeredText(canvas, cx, cy+10, "TIME’S UP", 36, true, color.White)
		r.centeredText(canvas, cx, cy+47, "SAY STOP OR TAP BELOW", 14, true, timerAccent)
	} else {
		arc(canvas, cx, cy, radius, -90, 360*timerProgress(timer, timerNow), 7, timerAccent)
		countdown := formatDuration(remaining)
		size := 64
		if len(countdown) > 5 {
			size = 48
		}
		r.centeredText(canvas, cx, cy+21, countdown, size, true, color.White)
	}
	label := firstNonEmpty(timer.Label, timer.Name)
	if label != "" {
		r.centeredText(canvas, cx, cy+radius+35, ellipsize(label, 26), 18, false, color.RGBA{205, 214, 226, 255})
	}
	button := timerStopBoundsIn(pane)
	buttonColor := color.RGBA{timerAccent.R, timerAccent.G, timerAccent.B, 48}
	if r.timerPressed {
		buttonColor = color.RGBA{timerAccent.R, timerAccent.G, timerAccent.B, 110}
	}
	roundedRect(canvas, button, 28, buttonColor)
	roundedRectOutline(canvas, button, 28, 2, color.RGBA{timerAccent.R, timerAccent.G, timerAccent.B, 190})
	buttonLabel := "CANCEL TIMER"
	if timer.Ringing {
		buttonLabel = "STOP TIMER"
	}
	r.centeredText(canvas, (button.Min.X+button.Max.X)/2, button.Min.Y+43, buttonLabel, 18, true, color.White)
}

func (r *renderer) drawNotification(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA) {
	item := r.state.Notification
	pane := rightPane(width, canvas.Rect.Dy())
	left, right := pane.Min.X, pane.Max.X
	noticeColor := accent
	if item.Priority == "critical" {
		noticeColor = color.RGBA{255, 92, 92, 255}
	}
	r.text(canvas, left, 52, fitTextToWidth("AWARENESS  ·  "+strings.ToUpper(firstNonEmpty(item.CameraName, item.Title)), r.faces.bold[16], pane.Dx()), 16, true, noticeColor)
	panel := image.Rect(left, 72, right, 260)
	roundedRect(canvas, panel, 18, color.RGBA{20, 27, 39, 245})
	if r.notificationImage != nil {
		drawCover(canvas, panel, r.notificationImage)
	} else {
		pulse := .5 + .5*math.Sin(seconds*2.2)
		circleOutline(canvas, (left+right)/2, 181, int(24+pulse*9), 4, color.RGBA{noticeColor.R, noticeColor.G, noticeColor.B, uint8(95 + pulse*80)})
		circle(canvas, (left+right)/2, 181, 9, noticeColor)
	}
	size, baseline, lineHeight, lines := notificationDescriptionLayout(r.faces, item.Description, pane.Dx())
	for index, value := range lines {
		r.text(canvas, left, baseline+index*lineHeight, fitTextToWidth(value, r.faces.regular[size], pane.Dx()), size, false, color.RGBA{215, 224, 236, 255})
	}
	if item.ExpiresAtUnixMS > 0 {
		remaining := float64(item.ExpiresAtUnixMS-r.currentTaterUnixMS(now)) / 90000
		progress := math.Max(0, math.Min(1, remaining))
		progressLeft := left
		line(canvas, progressLeft, pane.Max.Y-7, progressLeft+int(float64(right-progressLeft)*progress), pane.Max.Y-7, 3,
			color.RGBA{noticeColor.R, noticeColor.G, noticeColor.B, 160})
	}
}

func (r *renderer) drawLastEvent(canvas *image.RGBA, now time.Time, width int, seconds float64, accent color.RGBA) {
	r.drawCrownEventCard(canvas, now, width, seconds, accent, r.state.LastEvent, r.lastEventImage, false)
}

func (r *renderer) drawCrownEventCard(
	canvas *image.RGBA,
	now time.Time,
	width int,
	seconds float64,
	accent color.RGBA,
	item *show.Notification,
	eventImage image.Image,
	transient bool,
) {
	if item == nil {
		return
	}
	pane := crownPane(width, crownContextHeight)
	left, right := pane.Min.X, pane.Max.X
	eventColor := accent
	if item.Priority == "critical" {
		eventColor = color.RGBA{255, 92, 92, 255}
	}
	name := firstNonEmpty(item.CameraName, item.Title, "Recent activity")
	prefix := "LAST EVENT"
	if transient {
		prefix = "AWARENESS"
	}
	heading := prefix + "  ·  " + strings.ToUpper(name)
	r.text(canvas, left, 54, fitTextToWidth(heading, r.faces.bold[18], pane.Dx()), 18, true, eventColor)

	imageWidth := min(430, max(366, pane.Dx()-334))
	imageFrame := image.Rect(left, 78, left+imageWidth, 353)
	roundedRect(canvas, imageFrame, 18, color.RGBA{20, 27, 39, 245})
	if eventImage != nil {
		drawContain(canvas, imageFrame.Inset(6), eventImage)
	} else {
		pulse := .5 + .5*math.Sin(seconds*1.8)
		cx, cy := (imageFrame.Min.X+imageFrame.Max.X)/2, (imageFrame.Min.Y+imageFrame.Max.Y)/2
		circle(canvas, cx, cy, 44, color.RGBA{eventColor.R, eventColor.G, eventColor.B, 22})
		circleOutline(canvas, cx, cy, int(25+pulse*6), 4, color.RGBA{eventColor.R, eventColor.G, eventColor.B, uint8(110 + pulse*70)})
		circle(canvas, cx, cy, 9, eventColor)
	}

	separatorX := imageFrame.Max.X + 18
	line(canvas, separatorX, imageFrame.Min.Y, separatorX, 424, 1, color.RGBA{eventColor.R, eventColor.G, eventColor.B, 55})
	textLeft := separatorX + 22
	textWidth := max(1, right-textLeft)
	r.text(canvas, textLeft, 98, strings.ToUpper(firstNonEmpty(titleWords(item.Source), titleWords(item.Kind), "Awareness")), 14, true, eventColor)
	titleLines := wrapTextToWidth(name, r.faces.bold[26], textWidth)
	titleLineCount := min(2, len(titleLines))
	for index, value := range titleLines {
		if index >= titleLineCount {
			break
		}
		r.text(canvas, textLeft, 139+index*32, fitTextToWidth(value, r.faces.bold[26], textWidth), 26, true, color.White)
	}
	titleBottom := 139 + max(0, titleLineCount-1)*32
	meta := r.lastEventTimestamp(item.CreatedAtUnixMS, now)
	if meta == "" && transient {
		meta = "NOW"
	}
	if meta != "" {
		r.text(canvas, textLeft, titleBottom+29, fitTextToWidth(meta, r.faces.bold[16], textWidth), 16, true, color.RGBA{151, 164, 184, 255})
	}
	description := strings.TrimSpace(item.Description)
	if description == "" {
		description = name
	}
	descriptionY := titleBottom + 38
	if meta != "" {
		descriptionY = titleBottom + 64
	}
	descriptionLines := wrapTextToWidth(description, r.faces.regular[20], textWidth)
	maxLines := max(1, (416-descriptionY)/27)
	for index, value := range descriptionLines {
		if index >= maxLines {
			break
		}
		if index == maxLines-1 && len(descriptionLines) > maxLines {
			value = fitTextToWidth(value+"…", r.faces.regular[20], textWidth)
		}
		r.text(canvas, textLeft, descriptionY+index*27, value, 20, false, color.RGBA{215, 224, 236, 255})
	}
	if transient && item.ExpiresAtUnixMS > 0 {
		remaining := float64(item.ExpiresAtUnixMS-r.currentTaterUnixMS(now)) / 90000
		progress := math.Max(0, math.Min(1, remaining))
		line(canvas, left, pane.Max.Y-7, left+int(float64(right-left)*progress), pane.Max.Y-7, 3,
			color.RGBA{eventColor.R, eventColor.G, eventColor.B, 160})
	}
}
func (r *renderer) lastEventTimestamp(createdAtUnixMS int64, now time.Time) string {
	if createdAtUnixMS <= 0 {
		return ""
	}
	offset := time.Duration(r.state.TaterUTCOffset) * time.Second
	created := time.UnixMilli(createdAtUnixMS).UTC().Add(offset)
	localNow := r.localTime(now)
	day := func(value time.Time) time.Time {
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	}
	label := created.Format("JAN 2")
	switch delta := int(day(localNow).Sub(day(created)).Hours() / 24); delta {
	case 0:
		label = "TODAY"
	case 1:
		label = "YESTERDAY"
	}
	return label + "  ·  " + created.Format("3:04 PM")
}

func (r *renderer) drawIntercom(canvas *image.RGBA, accent color.RGBA) {
	centerX, centerY, radius, iconLeft, iconTop := intercomGeometry(canvas.Rect.Dx(), canvas.Rect.Dy())
	active := r.intercomPressed || r.state.Phase == "intercom"
	enabled := r.state.Connected && !r.state.Muted
	buttonColor := accent
	if active {
		buttonColor = color.RGBA{255, 105, 140, 255}
		circle(canvas, centerX, centerY, radius+7, color.RGBA{buttonColor.R, buttonColor.G, buttonColor.B, 32})
	}
	strength := uint8(74)
	if !enabled {
		strength = 32
	} else if active {
		strength = 145
	}
	circle(canvas, centerX, centerY, radius, color.RGBA{buttonColor.R, buttonColor.G, buttonColor.B, strength})
	circleOutline(canvas, centerX, centerY, radius-2, 3,
		color.RGBA{buttonColor.R, buttonColor.G, buttonColor.B, 200})
	if r.intercomIcon == nil {
		r.intercomIcon = newMicrophoneIcon()
	}
	iconAlpha := uint8(245)
	if !enabled {
		iconAlpha = 120
	}
	draw.DrawMask(canvas, image.Rect(iconLeft, iconTop, iconLeft+micIconWidth, iconTop+micIconHeight),
		image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: iconAlpha}), image.Point{},
		r.intercomIcon, image.Point{}, draw.Over)
}

// A small antialiased, filled microphone glyph reads more clearly than the
// previous framebuffer-drawn outline at Checkers' native pixel density.
func newMicrophoneIcon() *image.RGBA {
	icon := image.NewRGBA(image.Rect(0, 0, micIconWidth, micIconHeight))
	path := vector.NewRasterizer(micIconWidth, micIconHeight)
	// Capsule.
	path.MoveTo(20, 2)
	path.CubeTo(16.5, 2, 14, 4.8, 14, 8.5)
	path.LineTo(14, 20.5)
	path.CubeTo(14, 24.2, 16.5, 27, 20, 27)
	path.CubeTo(23.5, 27, 26, 24.2, 26, 20.5)
	path.LineTo(26, 8.5)
	path.CubeTo(26, 4.8, 23.5, 2, 20, 2)
	path.ClosePath()
	// One filled U shape forms the cradle without rough overlapping strokes.
	path.MoveTo(8, 20)
	path.LineTo(12, 20)
	path.LineTo(12, 26)
	path.CubeTo(12, 32.5, 15, 36, 20, 36)
	path.CubeTo(25, 36, 28, 32.5, 28, 26)
	path.LineTo(28, 20)
	path.LineTo(32, 20)
	path.LineTo(32, 26)
	path.CubeTo(32, 35, 27.5, 40, 20, 40)
	path.CubeTo(12.5, 40, 8, 35, 8, 26)
	path.ClosePath()
	// Stem and rounded base.
	path.MoveTo(18, 39)
	path.LineTo(22, 39)
	path.LineTo(22, 44)
	path.LineTo(18, 44)
	path.ClosePath()
	path.MoveTo(12, 43)
	path.LineTo(28, 43)
	path.CubeTo(29.5, 43, 30.5, 44, 30.5, 45.5)
	path.CubeTo(30.5, 47, 29.5, 47.5, 28, 47.5)
	path.LineTo(12, 47.5)
	path.CubeTo(10.5, 47.5, 9.5, 47, 9.5, 45.5)
	path.CubeTo(9.5, 44, 10.5, 43, 12, 43)
	path.ClosePath()
	path.Draw(icon, icon.Rect, image.NewUniform(color.White), image.Point{})
	return icon
}

func (r *renderer) text(canvas *image.RGBA, x, baseline int, value string, size int, bold bool, ink color.Color) {
	face := r.faces.regular[size]
	if bold {
		face = r.faces.bold[size]
	}
	drawer := font.Drawer{Dst: canvas, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(x, baseline)}
	drawer.DrawString(value)
}

func (r *renderer) centeredText(canvas *image.RGBA, centerX, baseline int, value string, size int, bold bool, ink color.Color) {
	face := r.faces.regular[size]
	if bold {
		face = r.faces.bold[size]
	}
	width := font.MeasureString(face, value).Round()
	r.text(canvas, centerX-width/2, baseline, value, size, bold, ink)
}

func wrapTextToWidth(value string, face font.Face, maxWidth int) []string {
	words := strings.Fields(value)
	lines := make([]string, 0, 4)
	lineValue := ""
	for _, word := range words {
		candidate := strings.TrimSpace(lineValue + " " + word)
		if lineValue != "" && font.MeasureString(face, candidate).Round() > maxWidth {
			lines = append(lines, lineValue)
			lineValue = word
		} else {
			lineValue = candidate
		}
	}
	if lineValue != "" {
		lines = append(lines, lineValue)
	}
	return lines
}

func notificationDescriptionLayout(faces *faceSet, description string, maxWidth int) (size, baseline, lineHeight int, lines []string) {
	// Use the larger type for brief alerts; longer descriptions still fit below
	// the image without covering the expiry indicator.
	for _, option := range []struct{ size, baseline, lineHeight, maxLines int }{
		{36, 296, 40, 3},
		{30, 290, 32, 4},
	} {
		wrapped := wrapTextToWidth(description, faces.regular[option.size], maxWidth)
		if len(wrapped) <= option.maxLines {
			return option.size, option.baseline, option.lineHeight, wrapped
		}
		if option.size == 30 {
			wrapped = wrapped[:option.maxLines]
			wrapped[len(wrapped)-1] = fitTextToWidth(wrapped[len(wrapped)-1]+"…", faces.regular[option.size], maxWidth)
			return option.size, option.baseline, option.lineHeight, wrapped
		}
	}
	return 30, 290, 32, nil
}

func (r *renderer) currentTaterUnixMS(now time.Time) int64 {
	if r.state.TaterTimeUnixMS <= 0 {
		return now.UnixMilli()
	}
	if r.received.IsZero() {
		return r.state.TaterTimeUnixMS
	}
	return r.state.TaterTimeUnixMS + now.Sub(r.received).Milliseconds()
}

func (r *renderer) notificationActive(now time.Time) bool {
	return r.state.Notification != nil && (r.state.Notification.ExpiresAtUnixMS <= 0 || r.currentTaterUnixMS(now) < r.state.Notification.ExpiresAtUnixMS)
}

func weatherDisplayKind(weather *show.Weather, local time.Time) string {
	if weather == nil {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(weather.ConditionKind))
	daylight := strings.ToLower(strings.TrimSpace(weather.Daylight))
	isNight := daylight == "night"
	if daylight != "day" && daylight != "night" {
		// Older Tater versions do not send daylight yet. Keep those devices
		// sensible using their already-synchronized local display clock.
		isNight = local.Hour() < 6 || local.Hour() >= 18
	}
	if !isNight {
		return kind
	}
	switch {
	case kind == "partly", strings.Contains(kind, "partly"):
		return "partly-night"
	case kind == "sun", kind == "sunny", strings.Contains(kind, "clear"):
		return "moon"
	default:
		return kind
	}
}

func (r *renderer) drawWeatherIcon(canvas *image.RGBA, cx, cy, radius int, kind string, seconds float64, accent color.RGBA) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	circle(canvas, cx, cy, radius+36, color.RGBA{accent.R, accent.G, accent.B, 9})
	circle(canvas, cx, cy, radius+18, color.RGBA{accent.R, accent.G, accent.B, 9})
	sun := kind == "sun" || kind == "partly" || strings.Contains(kind, "clear")
	moon := kind == "moon" || kind == "partly-night"
	cloud := (!sun && !moon) || kind == "partly" || kind == "partly-night"
	if kind == "fog" || kind == "wind" {
		cloud = false
	}
	drift := math.Sin(seconds*.42) * float64(radius) * .07
	if sun {
		sunX, sunY := cx, cy
		if kind == "partly" {
			sunX -= int(float64(radius) * .34)
			sunY -= int(float64(radius) * .23)
		}
		breath := 1 + .035*math.Sin(seconds*1.7)
		circle(canvas, sunX, sunY, int(float64(radius)*.72*breath), color.RGBA{255, 190, 75, 25})
		circle(canvas, sunX, sunY, int(float64(radius)*.38*breath), color.RGBA{255, 194, 79, 255})
		for index := 0; index < 8; index++ {
			angle := math.Pi*float64(index)/4 + seconds*.16
			line(canvas,
				sunX+int(math.Cos(angle)*float64(radius)*.48), sunY+int(math.Sin(angle)*float64(radius)*.48),
				sunX+int(math.Cos(angle)*float64(radius)*.64), sunY+int(math.Sin(angle)*float64(radius)*.64),
				3, color.RGBA{255, 191, 81, 255})
		}
	}
	if moon {
		moonX, moonY := cx, cy
		if kind == "partly-night" {
			moonX -= int(float64(radius) * .34)
			moonY -= int(float64(radius) * .23)
		}
		breath := 1 + .025*math.Sin(seconds*1.25)
		moonRadius := int(float64(radius) * .43 * breath)
		circle(canvas, moonX, moonY, int(float64(radius)*.78*breath), color.RGBA{150, 190, 255, 18})
		drawCrescent(canvas, moonX, moonY, moonRadius, color.RGBA{218, 231, 255, 255})
		for index := 0; index < 4; index++ {
			angle := float64(index)*1.73 + .5
			distance := float64(radius) * (.58 + .10*float64(index%2))
			alpha := uint8(125 + 80*(.5+.5*math.Sin(seconds*1.8+float64(index))))
			circle(canvas, moonX+int(math.Cos(angle)*distance), moonY+int(math.Sin(angle)*distance), 1+index%2,
				color.RGBA{198, 220, 255, alpha})
		}
	}
	if cloud {
		if kind == "cloud" || kind == "rain" || kind == "storm" {
			drawCloud(canvas, cx-int(float64(radius)*.42+drift*.45), cy-int(float64(radius)*.18),
				int(float64(radius)*.76), color.RGBA{122, 143, 169, 105})
			drawCloud(canvas, cx+int(float64(radius)*.40+drift*.30), cy-int(float64(radius)*.03),
				int(float64(radius)*.62), color.RGBA{144, 162, 185, 115})
		}
		cloudColor := color.RGBA{215, 226, 239, 255}
		if kind == "storm" {
			cloudColor = color.RGBA{139, 151, 175, 255}
		}
		drawCloud(canvas, cx+int(drift), cy+int(float64(radius)*.12), radius, cloudColor)
	}
	switch kind {
	case "rain", "storm":
		for index := 0; index < 6; index++ {
			progress := math.Mod(seconds*.72+float64(index)*.19, 1)
			x := cx + int(float64(radius)*(-.62+float64(index%4)*.40)+drift)
			y := cy + int(float64(radius)*(.50+progress*.76))
			line(canvas, x, y, x-int(float64(radius)*.10), y+int(float64(radius)*.24), 3,
				color.RGBA{83, 178, 255, uint8(210 * (1 - progress))})
		}
		if kind == "storm" && math.Sin(seconds*5.7) > .72 {
			line(canvas, cx+3, cy+20, cx-4, cy+37, 4, color.RGBA{255, 213, 79, 255})
			line(canvas, cx-4, cy+37, cx+4, cy+34, 4, color.RGBA{255, 213, 79, 255})
			line(canvas, cx+4, cy+34, cx-2, cy+51, 4, color.RGBA{255, 213, 79, 255})
		}
	case "snow":
		for index := 0; index < 7; index++ {
			progress := math.Mod(seconds*.24+float64(index)*.17, 1)
			x := cx + int(float64(radius)*(-.70+float64(index%4)*.45+math.Sin(seconds+float64(index))*.08))
			y := cy + int(float64(radius)*(.50+progress*.83))
			circle(canvas, x, y, 2, color.RGBA{190, 226, 255, 255})
		}
	case "fog", "wind":
		for index := -1; index <= 1; index++ {
			y := cy + int(float64(index*radius)*.27)
			travel := math.Sin(seconds*.50+float64(index)) * float64(radius) * .12
			span := float64(radius) * (.72 - math.Abs(float64(index))*.10)
			line(canvas, cx-int(span)+int(travel), y, cx+int(span)+int(travel), y, 3,
				color.RGBA{191, 208, 225, 220})
		}
	}
}

func drawCrescent(canvas *image.RGBA, cx, cy, radius int, ink color.RGBA) {
	if radius <= 0 {
		return
	}
	cutX, cutY := cx+radius*2/5, cy-radius/7
	cutRadius := radius * 9 / 10
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			cutDX, cutDY := x-cutX, y-cutY
			if cutDX*cutDX+cutDY*cutDY < cutRadius*cutRadius {
				continue
			}
			blendPixel(canvas, x, y, ink)
		}
	}
}

func drawCloud(canvas *image.RGBA, cx, cy, radius int, ink color.RGBA) {
	circle(canvas, cx-int(float64(radius)*.34), cy+int(float64(radius)*.10), int(float64(radius)*.31), ink)
	circle(canvas, cx, cy-int(float64(radius)*.08), int(float64(radius)*.42), ink)
	circle(canvas, cx+int(float64(radius)*.38), cy+int(float64(radius)*.12), int(float64(radius)*.28), ink)
	roundedRect(canvas, image.Rect(cx-int(float64(radius)*.62), cy+int(float64(radius)*.05),
		cx+int(float64(radius)*.63), cy+int(float64(radius)*.39)), max(2, int(float64(radius)*.16)), ink)
}

func main() {
	log.SetOutput(os.Stdout)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	screenDevice, err := linuxscreen.Open()
	if err != nil {
		log.Fatalf("screen: %v", err)
	}
	defer screenDevice.Close()
	_ = linuxscreen.SetBacklight(90)
	faces, err := newFaceSet()
	if err != nil {
		log.Fatalf("fonts: %v", err)
	}
	log.Printf("Tater Show %s using %s", version, screenDevice)
	client := newSocketClient()
	go client.run(ctx)
	// Match the original Checkers event loop: the small queue drops expendable
	// move samples in the evdev reader while Down and Up remain lossless.
	touchEvents := make(chan linuxinput.TouchEvent, 16)
	if touch, touchErr := linuxinput.Open(); touchErr != nil {
		log.Printf("touch unavailable: %v", touchErr)
	} else {
		defer touch.Close()
		log.Printf("touch using %s", touch)
		go func() {
			if err := touch.Run(ctx, touchEvents); err != nil && ctx.Err() == nil {
				log.Printf("touch stopped: %v", err)
			}
		}()
	}
	screenWidth, screenHeight := screenDevice.Canvas().Rect.Dx(), screenDevice.Canvas().Rect.Dy()
	spotScreen := screenWidth == screenHeight
	normalizedTarget := strings.ToLower(strings.TrimSpace(target))
	deviceName := "Tater Checkers"
	if normalizedTarget == "crown" {
		deviceName = "Tater Crown"
	} else if spotScreen {
		deviceName = "Tater Spot"
	}
	renderer := &renderer{
		faces: faces, spotScreen: spotScreen, target: normalizedTarget,
		screenWidth: screenWidth, screenHeight: screenHeight,
		state:          show.Snapshot{Phase: "offline", Message: "Connecting to Tater", DeviceName: deviceName},
		animationStart: time.Now(),
	}
	// Drive animation from wall time so dropped ticks slow the frame rate, not
	// the motion itself. Each native layout reports its animated regions; the
	// framebuffer path can therefore keep a steady cadence without repeatedly
	// rotating unchanged composition.
	ticker := time.NewTicker(displayFrameInterval(normalizedTarget))
	defer ticker.Stop()
	frameWindow := time.Now()
	frames := 0
	var renderTotal, presentTotal, convertTotal, panTotal, renderMax, presentMax time.Duration
	for {
		select {
		case <-ctx.Done():
			return
		case state := <-client.states:
			if state.Phase != renderer.state.Phase || notificationIdentity(state.Notification) != notificationIdentity(renderer.state.Notification) || notificationIdentity(state.LastEvent) != notificationIdentity(renderer.state.LastEvent) || timerIdentity(state.Timer) != timerIdentity(renderer.state.Timer) || mediaIdentity(state.Media) != mediaIdentity(renderer.state.Media) {
				renderer.animationStart = time.Now()
			}
			renderer.state = state
			renderer.received = time.Now()
			renderer.loadNotificationImage(ctx)
			renderer.loadLastEventImage(ctx)
			renderer.loadMediaImage(ctx)
		case event := <-touchEvents:
			handleQueuedTouch(renderer, client, event)
		case <-ticker.C:
			started := time.Now()
			if renderer.spotScreen {
				renderer.updateSpotHold(client, started)
			}
			renderer.render(screenDevice.Canvas(), started)
			rendered := time.Since(started)
			var presented, converted, panned time.Duration
			if len(renderer.presentRegions) > 0 {
				presentStarted := time.Now()
				if err := screenDevice.PresentRegions(renderer.presentRegions); err != nil {
					log.Fatalf("present screen: %v", err)
				}
				presented = time.Since(presentStarted)
				converted, panned = screenDevice.LastPresentTimings()
			}
			frames++
			renderTotal += rendered
			presentTotal += presented
			convertTotal += converted
			panTotal += panned
			if rendered > renderMax {
				renderMax = rendered
			}
			if presented > presentMax {
				presentMax = presented
			}
			if elapsed := time.Since(frameWindow); elapsed >= 15*time.Second {
				renderAverage := averageFrameDuration(renderTotal, frames)
				presentAverage := averageFrameDuration(presentTotal, frames)
				convertAverage := averageFrameDuration(convertTotal, frames)
				panAverage := averageFrameDuration(panTotal, frames)
				diagnostics := show.ScreenDiagnostics{
					FPS:           float64(frames) / elapsed.Seconds(),
					RenderAvgMS:   float64(renderAverage) / float64(time.Millisecond),
					RenderMaxMS:   float64(renderMax) / float64(time.Millisecond),
					PresentAvgMS:  float64(presentAverage) / float64(time.Millisecond),
					PresentMaxMS:  float64(presentMax) / float64(time.Millisecond),
					RotateAvgMS:   float64(convertAverage) / float64(time.Millisecond),
					PanAvgMS:      float64(panAverage) / float64(time.Millisecond),
					TouchDowns:    renderer.touchDowns,
					TouchMoves:    renderer.touchMoves,
					TouchUps:      renderer.touchUps,
					LastTouchX:    renderer.lastTouchX,
					LastTouchY:    renderer.lastTouchY,
					LastTouchAtMS: renderer.lastTouchAtMS,
					UpdatedAtMS:   time.Now().UnixMilli(),
				}
				log.Printf("[display] %.1f fps, render avg/max %s/%s, present avg/max %s/%s (rotate %s, pan %s)",
					diagnostics.FPS, renderAverage.Round(time.Millisecond), renderMax.Round(time.Millisecond),
					presentAverage.Round(time.Millisecond), presentMax.Round(time.Millisecond),
					convertAverage.Round(time.Millisecond), panAverage.Round(time.Millisecond))
				client.sendDiagnostics(diagnostics)
				frameWindow, frames, renderTotal, presentTotal, convertTotal, panTotal, renderMax, presentMax = time.Now(), 0, 0, 0, 0, 0, 0, 0
			}
		}
	}
}

func averageFrameDuration(total time.Duration, frames int) time.Duration {
	if frames <= 0 {
		return 0
	}
	return total / time.Duration(frames)
}

func displayFrameInterval(target string) time.Duration {
	return time.Second / 30
}

func drainTouchEvents(renderer *renderer, client *socketClient, events <-chan linuxinput.TouchEvent) int {
	handled := 0
	for {
		select {
		case event := <-events:
			if handleQueuedTouch(renderer, client, event) {
				handled++
			}
		default:
			return handled
		}
	}
}

func handleQueuedTouch(renderer *renderer, client *socketClient, event linuxinput.TouchEvent) bool {
	switch event.Kind {
	case linuxinput.Down:
		renderer.touchDowns++
	case linuxinput.Move:
		renderer.touchMoves++
	case linuxinput.Up:
		renderer.touchUps++
	}
	renderer.lastTouchX = event.X
	renderer.lastTouchY = event.Y
	renderer.lastTouchAtMS = time.Now().UnixMilli()
	// The current controls are tap/hold based and use the release coordinates;
	// movement samples cannot change their state. Ignoring them prevents panel
	// jitter from scheduling expensive full-frame Crown redraws.
	if event.Kind == linuxinput.Move {
		return false
	}
	beforeIntercom := renderer.intercomPressed
	beforeTimer := renderer.timerPressed
	handleTouch(renderer, client, event)
	if strings.EqualFold(strings.TrimSpace(renderer.target), "crown") {
		return beforeIntercom != renderer.intercomPressed || beforeTimer != renderer.timerPressed
	}
	return true
}

func handleTouch(renderer *renderer, client *socketClient, event linuxinput.TouchEvent) {
	if renderer.spotScreen {
		renderer.handleSpotTouch(client, event)
		return
	}
	width, height := renderer.showSize()
	centerX, centerY, radius, _, _ := intercomGeometry(width, height)
	intercom := inCircle(event.X, event.Y, centerX, centerY, radius)
	timerBounds := timerStopBounds(width, height)
	if renderer.isCrown() {
		timerBounds = timerStopBoundsIn(crownPane(width, height))
	}
	timer := image.Pt(event.X, event.Y).In(timerBounds)
	switch event.Kind {
	case linuxinput.Down:
		if renderer.state.Timer != nil && renderer.state.Timer.Active && timer {
			renderer.timerPressed = true
			return
		}
		if renderer.state.Connected && !renderer.state.Muted && intercom {
			renderer.intercomPressed = true
			client.send("intercom.start")
		}
	case linuxinput.Up:
		if renderer.timerPressed {
			renderer.timerPressed = false
			if timer {
				client.send("timer.stop")
			}
		}
		if renderer.intercomPressed {
			renderer.intercomPressed = false
			client.send("intercom.stop")
		}
	}
}

func (r *renderer) loadNotificationImage(ctx context.Context) {
	if r.state.Notification == nil || r.state.Notification.ImageURL == "" {
		r.notificationID, r.notificationImage = "", nil
		return
	}
	if r.notificationID == r.state.Notification.ID {
		return
	}
	r.notificationID, r.notificationImage = r.state.Notification.ID, nil
	parsed, err := url.Parse(r.state.Notification.ImageURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(contents) > 4*1024*1024 {
		return
	}
	decoded, _, err := image.Decode(bytes.NewReader(contents))
	if err == nil {
		r.notificationImage = decoded
	}
}

func (r *renderer) loadLastEventImage(ctx context.Context) {
	if r.state.LastEvent == nil || r.state.LastEvent.ImageURL == "" {
		r.lastEventID, r.lastEventImage = "", nil
		return
	}
	if r.lastEventID == r.state.LastEvent.ID {
		return
	}
	r.lastEventID = r.state.LastEvent.ID
	r.lastEventImage = loadLoopbackImage(ctx, r.state.LastEvent.ImageURL)
}

func (r *renderer) loadMediaImage(ctx context.Context) {
	if r.state.Media == nil || r.state.Media.ArtworkURL == "" {
		r.mediaID, r.mediaImage = "", nil
		return
	}
	identity := mediaIdentity(r.state.Media)
	if r.mediaID == identity {
		return
	}
	r.mediaID, r.mediaImage = identity, nil
	r.mediaImage = loadLoopbackImage(ctx, r.state.Media.ArtworkURL)
}

func loadLoopbackImage(ctx context.Context, rawURL string) image.Image {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(contents) > 4*1024*1024 {
		return nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(contents))
	if err != nil {
		return nil
	}
	return decoded
}

func fillGradient(destination *image.RGBA, top, bottom color.RGBA) {
	height := max(destination.Rect.Dy()-1, 1)
	for y := 0; y < destination.Rect.Dy(); y++ {
		ratio := float64(y) / float64(height)
		lineColor := mix(top, bottom, ratio)
		row := destination.Pix[y*destination.Stride : y*destination.Stride+destination.Rect.Dx()*4]
		row[0], row[1], row[2], row[3] = lineColor.R, lineColor.G, lineColor.B, 255
		for filled := 4; filled < len(row); {
			filled += copy(row[filled:], row[:filled])
		}
	}
}

func circle(destination *image.RGBA, cx, cy, radius int, value color.RGBA) {
	if radius <= 0 {
		return
	}
	squared := radius * radius
	for y := max(0, cy-radius); y <= min(destination.Rect.Dy()-1, cy+radius); y++ {
		for x := max(0, cx-radius); x <= min(destination.Rect.Dx()-1, cx+radius); x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= squared {
				blendPixel(destination, x, y, value)
			}
		}
	}
}

func circleOutline(destination *image.RGBA, cx, cy, radius, thickness int, value color.RGBA) {
	if radius <= 0 || thickness <= 0 {
		return
	}
	outer, inner := radius*radius, (radius-thickness)*(radius-thickness)
	for y := max(0, cy-radius); y <= min(destination.Rect.Dy()-1, cy+radius); y++ {
		for x := max(0, cx-radius); x <= min(destination.Rect.Dx()-1, cx+radius); x++ {
			distance := (x-cx)*(x-cx) + (y-cy)*(y-cy)
			if distance <= outer && distance >= inner {
				blendPixel(destination, x, y, value)
			}
		}
	}
}

func line(destination *image.RGBA, x0, y0, x1, y1, thickness int, value color.RGBA) {
	dx := math.Abs(float64(x1 - x0))
	dy := -math.Abs(float64(y1 - y0))
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := int(dx + dy)
	radius := max(1, thickness/2)
	for {
		circle(destination, x0, y0, radius, value)
		if x0 == x1 && y0 == y1 {
			break
		}
		twice := 2 * err
		if twice >= int(dy) {
			err += int(dy)
			x0 += sx
		}
		if twice <= int(dx) {
			err += int(dx)
			y0 += sy
		}
	}
}

func arc(destination *image.RGBA, cx, cy, radius int, startDegrees, sweepDegrees float64, thickness int, value color.RGBA) {
	if radius <= 0 || sweepDegrees == 0 {
		return
	}
	steps := max(2, int(math.Abs(sweepDegrees)*float64(radius)/90))
	previousX, previousY := 0, 0
	for step := 0; step <= steps; step++ {
		angle := (startDegrees + sweepDegrees*float64(step)/float64(steps)) * math.Pi / 180
		x := cx + int(math.Cos(angle)*float64(radius))
		y := cy + int(math.Sin(angle)*float64(radius))
		if step > 0 {
			line(destination, previousX, previousY, x, y, thickness, value)
		}
		previousX, previousY = x, y
	}
}

func roundedRect(destination *image.RGBA, rectangle image.Rectangle, radius int, value color.RGBA) {
	radius = min(max(radius, 0), min(rectangle.Dx(), rectangle.Dy())/2)
	// Color values throughout this renderer are straight-alpha RGBA. Convert to
	// NRGBA before compositing, and paint each row once so translucent controls
	// do not acquire a darker cross where two fill rectangles overlap.
	ink := image.NewUniform(color.NRGBA{R: value.R, G: value.G, B: value.B, A: value.A})
	for y := max(rectangle.Min.Y, destination.Rect.Min.Y); y < min(rectangle.Max.Y, destination.Rect.Max.Y); y++ {
		inset := 0
		fromEdge := min(y-rectangle.Min.Y, rectangle.Max.Y-1-y)
		if fromEdge < radius {
			vertical := float64(radius-fromEdge) - .5
			inset = int(math.Ceil(float64(radius) - math.Sqrt(float64(radius*radius)-vertical*vertical)))
		}
		row := image.Rect(rectangle.Min.X+inset, y, rectangle.Max.X-inset, y+1)
		draw.Draw(destination, row, ink, image.Point{}, draw.Over)
	}
}

func roundedRectOutline(destination *image.RGBA, rectangle image.Rectangle, radius, thickness int, value color.RGBA) {
	radius = min(radius, min(rectangle.Dx(), rectangle.Dy())/2)
	line(destination, rectangle.Min.X+radius, rectangle.Min.Y, rectangle.Max.X-radius, rectangle.Min.Y, thickness, value)
	line(destination, rectangle.Min.X+radius, rectangle.Max.Y-1, rectangle.Max.X-radius, rectangle.Max.Y-1, thickness, value)
	line(destination, rectangle.Min.X, rectangle.Min.Y+radius, rectangle.Min.X, rectangle.Max.Y-radius, thickness, value)
	line(destination, rectangle.Max.X-1, rectangle.Min.Y+radius, rectangle.Max.X-1, rectangle.Max.Y-radius, thickness, value)
	arc(destination, rectangle.Min.X+radius, rectangle.Min.Y+radius, radius, 180, 90, thickness, value)
	arc(destination, rectangle.Max.X-radius-1, rectangle.Min.Y+radius, radius, 270, 90, thickness, value)
	arc(destination, rectangle.Max.X-radius-1, rectangle.Max.Y-radius-1, radius, 0, 90, thickness, value)
	arc(destination, rectangle.Min.X+radius, rectangle.Max.Y-radius-1, radius, 90, 90, thickness, value)
}

func drawCover(destination *image.RGBA, rectangle image.Rectangle, source image.Image) {
	if source == nil {
		return
	}
	from := source.Bounds()
	scale := math.Max(float64(rectangle.Dx())/float64(from.Dx()), float64(rectangle.Dy())/float64(from.Dy()))
	width, height := int(float64(from.Dx())*scale), int(float64(from.Dy())*scale)
	for y := 0; y < rectangle.Dy(); y++ {
		sy := from.Min.Y + (y+(height-rectangle.Dy())/2)*from.Dy()/height
		for x := 0; x < rectangle.Dx(); x++ {
			sx := from.Min.X + (x+(width-rectangle.Dx())/2)*from.Dx()/width
			destination.Set(rectangle.Min.X+x, rectangle.Min.Y+y, source.At(sx, sy))
		}
	}
}

func drawContain(destination *image.RGBA, rectangle image.Rectangle, source image.Image) {
	if source == nil || rectangle.Empty() {
		return
	}
	from := source.Bounds()
	if from.Empty() {
		return
	}
	scale := math.Min(float64(rectangle.Dx())/float64(from.Dx()), float64(rectangle.Dy())/float64(from.Dy()))
	width := max(1, int(math.Round(float64(from.Dx())*scale)))
	height := max(1, int(math.Round(float64(from.Dy())*scale)))
	left := rectangle.Min.X + (rectangle.Dx()-width)/2
	top := rectangle.Min.Y + (rectangle.Dy()-height)/2
	for y := 0; y < height; y++ {
		sy := from.Min.Y + y*from.Dy()/height
		for x := 0; x < width; x++ {
			sx := from.Min.X + x*from.Dx()/width
			destination.Set(left+x, top+y, source.At(sx, sy))
		}
	}
}

func blendPixel(destination *image.RGBA, x, y int, source color.RGBA) {
	index := y*destination.Stride + x*4
	pixel := destination.Pix[index : index+4]
	if source.A == 255 {
		pixel[0], pixel[1], pixel[2], pixel[3] = source.R, source.G, source.B, 255
		return
	}
	alpha := uint32(source.A)
	inverse := uint32(255 - source.A)
	pixel[0] = uint8((uint32(source.R)*alpha + uint32(pixel[0])*inverse) / 255)
	pixel[1] = uint8((uint32(source.G)*alpha + uint32(pixel[1])*inverse) / 255)
	pixel[2] = uint8((uint32(source.B)*alpha + uint32(pixel[2])*inverse) / 255)
	pixel[3] = 255
}

func mix(left, right color.RGBA, amount float64) color.RGBA {
	amount = math.Max(0, math.Min(1, amount))
	return color.RGBA{uint8(float64(left.R)*(1-amount) + float64(right.R)*amount), uint8(float64(left.G)*(1-amount) + float64(right.G)*amount), uint8(float64(left.B)*(1-amount) + float64(right.B)*amount), 255}
}

func accentFor(phase string) color.RGBA {
	return accentForTheme(phase, "tater")
}

type displayPalette struct {
	primary   color.RGBA
	listening color.RGBA
	thinking  color.RGBA
	intercom  color.RGBA
	music     color.RGBA
}

func paletteForTheme(theme string) displayPalette {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "ocean":
		return displayPalette{
			primary: color.RGBA{62, 203, 255, 255}, listening: color.RGBA{73, 230, 194, 255},
			thinking: color.RGBA{102, 134, 255, 255}, intercom: color.RGBA{127, 224, 255, 255}, music: color.RGBA{73, 230, 194, 255},
		}
	case "violet":
		return displayPalette{
			primary: color.RGBA{176, 124, 255, 255}, listening: color.RGBA{239, 141, 255, 255},
			thinking: color.RGBA{117, 140, 255, 255}, intercom: color.RGBA{255, 181, 220, 255}, music: color.RGBA{141, 190, 255, 255},
		}
	case "forest":
		return displayPalette{
			primary: color.RGBA{117, 214, 110, 255}, listening: color.RGBA{85, 230, 183, 255},
			thinking: color.RGBA{212, 185, 95, 255}, intercom: color.RGBA{201, 242, 124, 255}, music: color.RGBA{92, 204, 180, 255},
		}
	case "sunset":
		return displayPalette{
			primary: color.RGBA{255, 111, 97, 255}, listening: color.RGBA{255, 173, 85, 255},
			thinking: color.RGBA{198, 116, 255, 255}, intercom: color.RGBA{255, 208, 106, 255}, music: color.RGBA{255, 145, 159, 255},
		}
	default:
		return displayPalette{
			primary: color.RGBA{255, 132, 48, 255}, listening: color.RGBA{52, 226, 183, 255},
			thinking: color.RGBA{154, 112, 255, 255}, intercom: color.RGBA{255, 105, 140, 255}, music: color.RGBA{86, 206, 255, 255},
		}
	}
}

func accentForTheme(phase, theme string) color.RGBA {
	palette := paletteForTheme(theme)
	switch phase {
	case "listening":
		return palette.listening
	case "thinking":
		return palette.thinking
	case "tool_call", "setup":
		return palette.primary
	case "speaking":
		return palette.primary
	case "intercom":
		return palette.intercom
	case "music":
		return palette.music
	case "error":
		return color.RGBA{255, 94, 94, 255}
	default:
		return palette.primary
	}
}

func phaseLabel(phase string) string {
	switch phase {
	case "thinking":
		return "THINKING"
	case "tool_call":
		return "WORKING"
	case "music":
		return "PLAYING"
	case "error":
		return "CHECK"
	case "offline":
		return "OFFLINE"
	default:
		return "TATER"
	}
}

func weatherColor(kind string) color.RGBA {
	kind = strings.ToLower(kind)
	switch {
	case strings.Contains(kind, "moon"), strings.Contains(kind, "night"):
		return color.RGBA{144, 183, 255, 255}
	case strings.Contains(kind, "storm"), strings.Contains(kind, "lightning"):
		return color.RGBA{165, 126, 255, 255}
	case strings.Contains(kind, "rain"), strings.Contains(kind, "drizzle"):
		return color.RGBA{83, 156, 255, 255}
	case strings.Contains(kind, "snow"):
		return color.RGBA{194, 229, 255, 255}
	case strings.Contains(kind, "cloud"), strings.Contains(kind, "overcast"):
		return color.RGBA{166, 184, 207, 255}
	default:
		return color.RGBA{255, 194, 76, 255}
	}
}

func titleWords(value string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.TrimSpace(value)))
	for index := range words {
		if words[index] != "" {
			words[index] = strings.ToUpper(words[index][:1]) + words[index][1:]
		}
	}
	return strings.Join(words, " ")
}

func ellipsize(value string, limit int) string {
	value = strings.TrimSpace(value)
	characters := []rune(value)
	if len(characters) <= limit {
		return value
	}
	return strings.TrimSpace(string(characters[:limit-1])) + "…"
}

func fitTextToWidth(value string, face font.Face, width int) string {
	value = strings.TrimSpace(value)
	if font.MeasureString(face, value).Round() <= width {
		return value
	}
	characters := []rune(value)
	low, high := 0, len(characters)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := strings.TrimSpace(string(characters[:middle])) + "…"
		if font.MeasureString(face, candidate).Round() <= width {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if low == 0 {
		return "…"
	}
	return strings.TrimSpace(string(characters[:low])) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func trimFeelsLike(value string) string {
	value = strings.TrimSpace(value)
	const prefix = "feels like "
	if len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix) {
		return strings.TrimSpace(value[len(prefix):])
	}
	return value
}

func formatDuration(duration time.Duration) string {
	total := int64(math.Ceil(duration.Seconds()))
	if total < 0 {
		total = 0
	}
	if total >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", total/3600, total%3600/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func timerRemaining(timer *show.Timer, now time.Time) time.Duration {
	if timer == nil {
		return 0
	}
	remaining := time.Duration(timer.RemainingMS) * time.Millisecond
	if timer.DeadlineUnixMS > 0 && !timer.Ringing {
		remaining = time.UnixMilli(timer.DeadlineUnixMS).Sub(now)
	}
	if remaining < 0 {
		return 0
	}
	return remaining
}

func timerProgress(timer *show.Timer, now time.Time) float64 {
	if timer == nil || timer.Ringing || timer.OriginalDurationMS <= 0 {
		return 0
	}
	progress := float64(timerRemaining(timer, now).Milliseconds()) / float64(timer.OriginalDurationMS)
	return math.Max(0, math.Min(1, progress))
}

func notificationIdentity(notification *show.Notification) string {
	if notification == nil {
		return ""
	}
	return notification.ID
}

func timerIdentity(timer *show.Timer) string {
	if timer == nil {
		return ""
	}
	return fmt.Sprintf("%s:%t", timer.ID, timer.Ringing)
}

func mediaIdentity(media *show.Media) string {
	if media == nil || !media.Active {
		return ""
	}
	return strings.Join([]string{media.ArtworkURL, media.Title, media.Artist, media.Album}, "\x00")
}

func inCircle(x, y, centerX, centerY, radius int) bool {
	return (x-centerX)*(x-centerX)+(y-centerY)*(y-centerY) <= radius*radius
}
