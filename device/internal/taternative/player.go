package taternative

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/TaterTotterson/Tater-Echo-Firmware/pkg/speaker"
	"github.com/hajimehoshi/go-mp3"
)

const (
	playbackRate     = 48000
	playbackPeriod   = 4096
	maxAudioDownload = 64 * 1024 * 1024
	streamAudioRead  = 32 * 1024
	maxWakeSoundRaw  = 2 * 1024 * 1024
	maxWakeSoundPCM  = 8 * 1024 * 1024
	defaultWakeCache = "/data/local/share/tater/wake-sounds"
)

const unknownWAVChunkSize = uint32(0xffffffff)

var builtInWakeSounds = map[string]string{
	"default":                "wake_word_triggered",
	"blip2":                  "blip2",
	"message-notification-4": "message-notification-4",
	"notification-ding":      "notification-ding",
	"notification-squeak":    "notification-squeak",
	"phone-chime":            "phone-chime",
	"pop-up-sound":           "pop-up-sound",
	"short-definite-fart":    "short-definite-fart",
	"star_treck_communications_start_transmission": "star_treck_communications_start_transmission",
	"star_treck_computer_work_beep":                "star_treck_computer_work_beep",
	"tater_notify_digital_blip":                    "tater_notify_digital_blip",
	"turning-off-microphone-percussion-1":          "turning-off-microphone-percussion-1",
	"wake_word_triggered":                          "wake_word_triggered",
	"waterdrop":                                    "waterdrop",
}

// LocalPlayer downloads WAV/MP3 audio, decodes it to the Echo speaker's
// 48 kHz mono S16_LE format and feeds the existing voice/music planes.
type LocalPlayer struct {
	Speaker       speaker.Speaker
	HTTP          *http.Client
	voiceMu       sync.Mutex
	alarmMu       sync.Mutex
	alarmCancel   context.CancelFunc
	mediaCacheDir string

	wakeMu               sync.RWMutex
	wakeCacheDir         string
	wakeEnabled          bool
	wakeID               string
	wakeURL              string
	wakePCM              []byte
	wakeGeneration       uint64
	wakeDownloadCancel   context.CancelFunc
	wakeDownloadRunning  bool
	wakeDownloadURL      string
	wakeCacheHits        uint64
	wakeCacheWrites      uint64
	wakeCacheFailures    uint64
	wakeDownloadFailures uint64
	wakeLastError        string
}

type timedDucker interface {
	SetDuckRamp(db float64, duration time.Duration)
}

func setSpeakerDuck(spk speaker.Speaker, db float64, duration time.Duration) {
	if timed, ok := spk.(timedDucker); ok {
		timed.SetDuckRamp(db, duration)
		return
	}
	spk.SetDuck(db)
}

// voiceIdleWaiter is implemented by the hardware speaker. Keeping it
// optional lets the player remain usable with simple test speakers while the
// Echo can wait for its deep playback queue and ALSA buffer to become silent.
type voiceIdleWaiter interface {
	WaitVoiceIdle(context.Context) error
}

func NewLocalPlayer(spk speaker.Speaker) *LocalPlayer {
	p := &LocalPlayer{
		Speaker:       spk,
		HTTP:          &http.Client{Timeout: 90 * time.Second},
		wakeCacheDir:  defaultWakeCache,
		mediaCacheDir: "/data/local/share/tater/media-cache",
		wakeID:        "no_sound",
	}
	return p
}

func (p *LocalPlayer) PlayVoice(ctx context.Context, req PlayRequest) error {
	if p.Speaker == nil {
		return errors.New("speaker unavailable")
	}
	p.voiceMu.Lock()
	defer p.voiceMu.Unlock()
	duckDB := 0.0
	duckStarted := false
	release := time.Duration(0)
	startAudio := func() {}
	if target := intValue(req.Ducking["target_percent"], 100); target < 100 {
		if target <= 0 {
			duckDB = -96
		} else {
			duckDB = 20 * math.Log10(float64(target)/100)
		}
		attack := time.Duration(clamp(intValue(req.Ducking["attack_ms"], 150), 0, 10000)) * time.Millisecond
		release = time.Duration(clamp(intValue(req.Ducking["release_ms"], 350), 0, 10000)) * time.Millisecond
		startAudio = func() {
			if duckStarted {
				return
			}
			duckStarted = true
			setSpeakerDuck(p.Speaker, duckDB, attack)
		}
	}
	defer func() {
		if duckStarted {
			setSpeakerDuck(p.Speaker, 0, release)
		}
	}()
	if err := p.fetchAndPumpVoice(ctx, req.URL, startAudio); err != nil {
		p.Speaker.Flush()
		p.Speaker.EndStream()
		return err
	}
	p.Speaker.EndStream()
	if waiter, ok := p.Speaker.(voiceIdleWaiter); ok {
		if err := waiter.WaitVoiceIdle(ctx); err != nil {
			p.Speaker.Flush()
			return err
		}
	}
	return nil
}

// fetchAndPumpVoice incrementally decodes PCM WAV replies so a satellite can
// begin filling its speaker queue while a TTS server is still producing the
// rest of the response. Formats such as MP3 retain the bounded whole-file
// decoder because their current decoder is not safe to resume chunk by chunk.
func (p *LocalPlayer) fetchAndPumpVoice(ctx context.Context, rawURL string, startAudio func()) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create audio request: %w", err)
	}
	response, err := p.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("download audio: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download audio: HTTP %d", response.StatusCode)
	}

	bounded := &boundedAudioReader{reader: response.Body, remaining: maxAudioDownload}
	reader := bufio.NewReaderSize(bounded, streamAudioRead)
	header, peekErr := reader.Peek(12)
	if peekErr == nil && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE" {
		return p.streamWAV(ctx, reader, 100, startAudio)
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read audio: %w", err)
	}
	contentType := strings.ToLower(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	pcm, err := decodeAudioChannel(body, contentType, rawURL, "mono")
	if err != nil {
		return err
	}
	startAudio()
	return p.pump(ctx, pcm, 100)
}

type boundedAudioReader struct {
	reader    io.Reader
	remaining int64
}

func (r *boundedAudioReader) Read(dst []byte) (int, error) {
	if r.remaining <= 0 {
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("audio exceeds %d bytes", maxAudioDownload)
		}
		return 0, err
	}
	if int64(len(dst)) > r.remaining {
		dst = dst[:r.remaining]
	}
	n, err := r.reader.Read(dst)
	r.remaining -= int64(n)
	return n, err
}

type voiceStreamResampler struct {
	sourceRate  int
	targetRate  int
	inputOffset int64
	nextOutput  int64
	samples     []int16
}

func (r *voiceStreamResampler) append(samples []int16, final bool) []byte {
	if len(samples) > 0 {
		r.samples = append(r.samples, samples...)
	}
	if r.sourceRate <= 0 || r.targetRate <= 0 || len(r.samples) == 0 {
		return nil
	}
	// Keep the phase in absolute stream coordinates. Re-basing a floating-point
	// phase after every HTTP read makes 44.1 kHz output depend on chunk sizes.
	ratio := float64(r.sourceRate) / float64(r.targetRate)
	out := make([]byte, 0, maxInt(len(samples)*r.targetRate/r.sourceRate, 1)*2)
	inputEnd := r.inputOffset + int64(len(r.samples))
	for {
		position := float64(r.nextOutput) * ratio
		left := int64(position)
		if final {
			if position >= float64(inputEnd) {
				break
			}
		} else if left >= inputEnd-1 {
			break
		}

		localLeft := int(left - r.inputOffset)
		value := r.samples[localLeft]
		if left < inputEnd-1 {
			fraction := position - float64(left)
			interpolated := float64(r.samples[localLeft]) + fraction*float64(int(r.samples[localLeft+1])-int(r.samples[localLeft]))
			value = int16(math.Round(interpolated))
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(value))
		r.nextOutput++
	}

	nextPosition := float64(r.nextOutput) * ratio
	drop := int(int64(nextPosition) - r.inputOffset)
	if drop > 0 {
		if drop >= len(r.samples) {
			r.inputOffset += int64(len(r.samples))
			r.samples = r.samples[:0]
		} else {
			copy(r.samples, r.samples[drop:])
			r.samples = r.samples[:len(r.samples)-drop]
			r.inputOffset += int64(drop)
		}
	}
	return out
}

func parseStreamingWAVFormat(chunk []byte) (channels, rate, bits int, err error) {
	if len(chunk) < 16 {
		return 0, 0, 0, errors.New("short WAV fmt chunk")
	}
	format := int(binary.LittleEndian.Uint16(chunk[0:2]))
	channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
	rate = int(binary.LittleEndian.Uint32(chunk[4:8]))
	bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
	if format != 1 || bits != 16 || channels < 1 || channels > 8 || rate < 8000 || rate > 192000 {
		return 0, 0, 0, fmt.Errorf("unsupported WAV format=%d channels=%d rate=%d bits=%d", format, channels, rate, bits)
	}
	return channels, rate, bits, nil
}

func (p *LocalPlayer) streamWAV(ctx context.Context, reader io.Reader, volume int, startAudio func()) error {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return fmt.Errorf("read WAV header: %w", err)
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return errors.New("invalid WAV header")
	}

	var channels, rate int
	for {
		var chunkHeader [8]byte
		if _, err := io.ReadFull(reader, chunkHeader[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("WAV has no data chunk")
			}
			return fmt.Errorf("read WAV chunk header: %w", err)
		}
		name := string(chunkHeader[:4])
		rawSize := binary.LittleEndian.Uint32(chunkHeader[4:])
		if rawSize == unknownWAVChunkSize && name != "data" {
			return errors.New("invalid WAV chunk length")
		}

		switch name {
		case "fmt ":
			if rawSize > maxAudioDownload {
				return errors.New("invalid WAV chunk length")
			}
			chunk := make([]byte, int(rawSize))
			if _, err := io.ReadFull(reader, chunk); err != nil {
				return fmt.Errorf("read WAV fmt chunk: %w", err)
			}
			var err error
			channels, rate, _, err = parseStreamingWAVFormat(chunk)
			if err != nil {
				return err
			}
			if rawSize%2 != 0 {
				if _, err := io.CopyN(io.Discard, reader, 1); err != nil {
					return fmt.Errorf("read WAV chunk padding: %w", err)
				}
			}
		case "data":
			if channels == 0 || rate == 0 {
				return errors.New("WAV data chunk precedes format")
			}
			if rawSize == unknownWAVChunkSize {
				return p.streamPCM16(ctx, reader, channels, rate, volume, startAudio)
			}
			limited := &io.LimitedReader{R: reader, N: int64(rawSize)}
			err := p.streamPCM16(ctx, limited, channels, rate, volume, startAudio)
			if err != nil {
				return err
			}
			if limited.N != 0 {
				return errors.New("invalid WAV chunk length")
			}
			return nil
		default:
			if rawSize > maxAudioDownload {
				return errors.New("invalid WAV chunk length")
			}
			skip := int64(rawSize) + int64(rawSize%2)
			if _, err := io.CopyN(io.Discard, reader, skip); err != nil {
				return fmt.Errorf("read WAV chunk %q: %w", name, err)
			}
		}
	}
}

func (p *LocalPlayer) streamPCM16(ctx context.Context, reader io.Reader, channels, rate, volume int, startAudio func()) error {
	frameBytes := channels * 2
	buffer := make([]byte, streamAudioRead)
	carry := make([]byte, 0, frameBytes)
	pending := make([]byte, 0, playbackPeriod*2)
	resampler := &voiceStreamResampler{sourceRate: rate, targetRate: playbackRate}
	wrote := false
	pumpPCM := func(pcm []byte, final bool) error {
		pending = append(pending, pcm...)
		complete := len(pending) - len(pending)%playbackPeriod
		if complete > 0 {
			startAudio()
			if err := p.pump(ctx, pending[:complete], volume); err != nil {
				return err
			}
			wrote = true
		}
		remaining := len(pending) - complete
		copy(pending[:remaining], pending[complete:])
		pending = pending[:remaining]
		if final && len(pending) > 0 {
			startAudio()
			if err := p.pump(ctx, pending, volume); err != nil {
				return err
			}
			wrote = true
			pending = pending[:0]
		}
		return nil
	}
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			data := append(carry, buffer[:n]...)
			usable := len(data) - len(data)%frameBytes
			if usable > 0 {
				pcm := resampler.append(selectS16Channel(data[:usable], channels, "mono"), false)
				if len(pcm) > 0 {
					if err := pumpPCM(pcm, false); err != nil {
						return err
					}
				}
			}
			carry = append(carry[:0], data[usable:]...)
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("read WAV data: %w", readErr)
			}
			break
		}
	}
	if err := pumpPCM(resampler.append(nil, true), true); err != nil {
		return err
	}
	if !wrote {
		return errors.New("WAV data chunk is empty")
	}
	return nil
}

// ConfigureWakeSound applies Tater's live wake-sound settings and prepares
// the selected sound in the background. Built-ins are bundled from the same
// assets as the ESP32 satellites so they work without DNS or Internet access.
// Custom URLs are decoded and cached on disk so reconnects do not redownload.
func (p *LocalPlayer) ConfigureWakeSound(values map[string]any) error {
	p.wakeMu.Lock()
	enabled := p.wakeEnabled
	id := p.wakeID
	customURL := p.wakeURL
	if value, ok := values["wake_sound_enabled"]; ok {
		enabled = boolValue(value)
	}
	if value, ok := values["wake_sound"]; ok {
		id = strings.ToLower(strings.TrimSpace(stringValue(value)))
	}
	if value, ok := values["wake_sound_url"]; ok {
		customURL = strings.TrimSpace(stringValue(value))
	}
	if id == "" {
		id = "no_sound"
	}
	resolved := ""
	switch {
	case !enabled || id == "no_sound":
		enabled = false
	case id == "custom":
		resolved = customURL
	default:
		if embeddedID := builtInWakeSounds[id]; embeddedID != "" {
			resolved = "embedded:" + embeddedID
		}
	}
	if enabled && resolved == "" {
		p.wakeMu.Unlock()
		return fmt.Errorf("wake sound %q is not available", id)
	}
	if resolved != "" && !strings.HasPrefix(resolved, "embedded:") {
		parsed, err := url.Parse(resolved)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			p.wakeMu.Unlock()
			return fmt.Errorf("wake sound URL must use http:// or https://")
		}
	}

	unchanged := p.wakeEnabled == enabled && p.wakeID == id && p.wakeURL == resolved
	p.wakeEnabled = enabled
	p.wakeID = id
	p.wakeURL = resolved
	if unchanged && (!enabled || len(p.wakePCM) > 0 || p.wakeDownloadRunning) {
		p.wakeMu.Unlock()
		return nil
	}
	p.wakeGeneration++
	generation := p.wakeGeneration
	if p.wakeDownloadCancel != nil {
		p.wakeDownloadCancel()
		p.wakeDownloadCancel = nil
	}
	p.wakePCM = nil
	p.wakeLastError = ""
	if !enabled {
		p.wakeDownloadRunning = false
		p.wakeDownloadURL = ""
		p.wakeMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.wakeDownloadCancel = cancel
	p.wakeDownloadRunning = true
	p.wakeDownloadURL = resolved
	p.wakeMu.Unlock()
	go p.prepareWakeSound(ctx, generation, resolved)
	return nil
}

func (p *LocalPlayer) prepareWakeSound(ctx context.Context, generation uint64, rawURL string) {
	pcm, cacheHit, err := p.loadWakeSound(ctx, rawURL)
	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	if generation != p.wakeGeneration {
		return
	}
	p.wakeDownloadCancel = nil
	p.wakeDownloadRunning = false
	p.wakeDownloadURL = ""
	if err != nil {
		p.wakeDownloadFailures++
		p.wakeLastError = err.Error()
		return
	}
	if cacheHit {
		p.wakeCacheHits++
	} else {
		p.wakeCacheWrites++
	}
	p.wakePCM = pcm
	p.wakeLastError = ""
}

func (p *LocalPlayer) loadWakeSound(ctx context.Context, rawURL string) ([]byte, bool, error) {
	if embeddedID, ok := strings.CutPrefix(rawURL, "embedded:"); ok {
		encoded, exists := embeddedWakeSoundsGzipBase64[embeddedID]
		if !exists {
			return nil, false, fmt.Errorf("embedded wake sound %q is unavailable", embeddedID)
		}
		compressed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, false, fmt.Errorf("decode embedded wake sound %q: %w", embeddedID, err)
		}
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, false, fmt.Errorf("open embedded wake sound %q: %w", embeddedID, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, maxWakeSoundRaw+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, false, fmt.Errorf("read embedded wake sound %q: %w", embeddedID, readErr)
		}
		if closeErr != nil {
			return nil, false, fmt.Errorf("close embedded wake sound %q: %w", embeddedID, closeErr)
		}
		if len(raw) > maxWakeSoundRaw {
			return nil, false, fmt.Errorf("embedded wake sound %q exceeds %d bytes", embeddedID, maxWakeSoundRaw)
		}
		pcm, err := decodeAudio(raw, "audio/wav", embeddedID+".wav")
		if err != nil {
			return nil, false, fmt.Errorf("decode embedded wake sound %q: %w", embeddedID, err)
		}
		if len(pcm) == 0 || len(pcm) > maxWakeSoundPCM {
			return nil, false, fmt.Errorf("decoded wake sound exceeds %d bytes", maxWakeSoundPCM)
		}
		return pcm, true, nil
	}

	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(rawURL)))
	cachePath := filepath.Join(p.wakeCacheDir, digest+".pcm")
	if cached, err := os.ReadFile(cachePath); err == nil {
		if len(cached) > 0 && len(cached) <= maxWakeSoundPCM && len(cached)%2 == 0 {
			return cached, true, nil
		}
		p.wakeMu.Lock()
		p.wakeCacheFailures++
		p.wakeMu.Unlock()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create wake sound request: %w", err)
	}
	response, err := p.HTTP.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("download wake sound: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("download wake sound: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxWakeSoundRaw+1))
	if err != nil {
		return nil, false, fmt.Errorf("read wake sound: %w", err)
	}
	if len(raw) > maxWakeSoundRaw {
		return nil, false, fmt.Errorf("wake sound exceeds %d bytes", maxWakeSoundRaw)
	}
	contentType := strings.ToLower(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	pcm, err := decodeAudio(raw, contentType, rawURL)
	if err != nil {
		return nil, false, err
	}
	if len(pcm) == 0 || len(pcm) > maxWakeSoundPCM {
		return nil, false, fmt.Errorf("decoded wake sound exceeds %d bytes", maxWakeSoundPCM)
	}
	if err := os.MkdirAll(p.wakeCacheDir, 0o755); err != nil {
		p.wakeMu.Lock()
		p.wakeCacheFailures++
		p.wakeMu.Unlock()
		return pcm, false, nil
	}
	temporary, err := os.CreateTemp(p.wakeCacheDir, ".wake-*.pcm")
	if err != nil {
		p.wakeMu.Lock()
		p.wakeCacheFailures++
		p.wakeMu.Unlock()
		return pcm, false, nil
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err = temporary.Write(pcm); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryName, cachePath)
	}
	if err != nil {
		p.wakeMu.Lock()
		p.wakeCacheFailures++
		p.wakeMu.Unlock()
	}
	return pcm, false, nil
}

// PlayWakeSound queues the prepared sound locally without delaying voice.start.
// False means wake sounds are disabled or the newly selected asset is still
// downloading. Barge-in callers deliberately skip this hook altogether.
func (p *LocalPlayer) PlayWakeSound() bool {
	p.wakeMu.RLock()
	if !p.wakeEnabled || len(p.wakePCM) == 0 || p.Speaker == nil {
		p.wakeMu.RUnlock()
		return false
	}
	pcm := p.wakePCM
	p.wakeMu.RUnlock()
	go func() {
		p.voiceMu.Lock()
		defer p.voiceMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.pump(ctx, pcm, 100); err != nil {
			p.Speaker.Flush()
		}
		p.Speaker.EndStream()
	}()
	return true
}

// PlayEmbeddedSound plays one bundled wake-sound asset synchronously. The
// physical setup-reset gesture uses this for the same audible confirmation as
// the ESP satellites, and its caller supplies a short timeout so a damaged
// asset can never prevent recovery.
func (p *LocalPlayer) PlayEmbeddedSound(ctx context.Context, id string) error {
	if p.Speaker == nil {
		return errors.New("speaker unavailable")
	}
	pcm, _, err := p.loadWakeSound(ctx, "embedded:"+strings.TrimSpace(id))
	if err != nil {
		return err
	}
	p.voiceMu.Lock()
	defer p.voiceMu.Unlock()
	if err := p.pump(ctx, pcm, 100); err != nil {
		p.Speaker.Flush()
		p.Speaker.EndStream()
		return err
	}
	p.Speaker.EndStream()
	if waiter, ok := p.Speaker.(voiceIdleWaiter); ok {
		if err := waiter.WaitVoiceIdle(ctx); err != nil {
			p.Speaker.Flush()
			return err
		}
	}
	return nil
}

// WakeSoundStatus is shaped like the ESP32 wake-engine diagnostics consumed by
// Tater's satellite details page.
func (p *LocalPlayer) WakeSoundStatus() map[string]any {
	p.wakeMu.RLock()
	defer p.wakeMu.RUnlock()
	return map[string]any{
		"wake_sound_download_running":  p.wakeDownloadRunning,
		"wake_sound_download_url":      p.wakeDownloadURL,
		"wake_sound_cache_hits":        p.wakeCacheHits,
		"wake_sound_cache_writes":      p.wakeCacheWrites,
		"wake_sound_cache_failures":    p.wakeCacheFailures,
		"wake_sound_download_failures": p.wakeDownloadFailures,
		"wake_sound_ready":             p.wakeEnabled && len(p.wakePCM) > 0,
		"wake_sound_id":                p.wakeID,
		"wake_sound_last_error":        p.wakeLastError,
	}
}

func (p *LocalPlayer) StopVoice() { p.Speaker.Flush() }

func (p *LocalPlayer) PlayMedia(ctx context.Context, req MediaRequest) error {
	if p.Speaker == nil {
		return errors.New("speaker unavailable")
	}
	volume := clamp(req.VolumePercent, 0, 100)
	setSpeakerDuck(p.Speaker, 0, 0)
	asset, err := p.startMediaAsset(ctx, req.URL, req.Channel)
	if err != nil {
		return err
	}
	defer asset.Close()
	startFrame := int64(maxInt(req.StartPositionMS, 0)) * playbackRate / 1000
	if _, err := asset.waitFrames(ctx, startFrame+mediaPrebufferFrames); err != nil {
		return err
	}
	defer p.Speaker.EndMusicStream()
	cursor := startFrame
	for {
		samples, ended, _, err := asset.readFrames(ctx, &cursor, playbackPeriod/2, req.Loop)
		if err != nil {
			p.Speaker.FlushMusic()
			return err
		}
		if len(samples) > 0 {
			chunk := resampleMediaBlock(samples, len(samples))
			if volume != 100 {
				chunk = scalePCM(chunk, volume)
			}
			if err := p.Speaker.PumpMusic(chunk); err != nil {
				p.Speaker.FlushMusic()
				return err
			}
		}
		if ended && !req.Loop {
			return nil
		}
	}
}

func (p *LocalPlayer) StopMedia() {
	p.Speaker.FlushMusic()
	setSpeakerDuck(p.Speaker, 0, 0)
}

// SetTimerAlarm starts or stops the local repeating timer-finished sound. The
// embedded zen chime matches the ESP32 satellites and includes its own quiet
// listening gap before each repeat.
func (p *LocalPlayer) SetTimerAlarm(active bool) {
	p.alarmMu.Lock()
	if p.alarmCancel != nil {
		p.alarmCancel()
		p.alarmCancel = nil
	}
	if !active || p.Speaker == nil {
		p.alarmMu.Unlock()
		if !active && p.Speaker != nil {
			p.Speaker.Flush()
			p.Speaker.EndStream()
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.alarmCancel = cancel
	p.alarmMu.Unlock()
	go func() {
		defer p.Speaker.EndStream()
		sound := timerSound()
		for {
			if err := p.pump(ctx, sound, 80); err != nil {
				return
			}
		}
	}()
}

func legacyTimerTone() []byte {
	const duration = 900 * time.Millisecond
	samples := int(duration.Seconds() * playbackRate)
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		phase := float64(i) / playbackRate
		frequency := 660.0
		if (i/(playbackRate/6))%2 == 1 {
			frequency = 880
		}
		envelope := 1.0
		cycle := i % (playbackRate / 6)
		if cycle < 240 {
			envelope = float64(cycle) / 240
		}
		value := int16(math.Sin(2*math.Pi*frequency*phase) * 9000 * envelope)
		binary.LittleEndian.PutUint16(out[i*2:], uint16(value))
	}
	return out
}

func (p *LocalPlayer) pump(ctx context.Context, pcm []byte, volume int) error {
	for offset := 0; offset < len(pcm); {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := offset + playbackPeriod
		if end > len(pcm) {
			end = len(pcm)
		}
		chunk := pcm[offset:end]
		if len(chunk)%2 != 0 {
			chunk = chunk[:len(chunk)-1]
		}
		if len(chunk) < playbackPeriod {
			period := make([]byte, playbackPeriod)
			copy(period, chunk)
			chunk = period
		}
		if volume != 100 {
			chunk = scalePCM(chunk, volume)
		}
		if err := p.Speaker.PumpPeriod(chunk); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func scalePCM(raw []byte, percent int) []byte {
	out := append([]byte(nil), raw...)
	factor := float64(clamp(percent, 0, 100)) / 100
	for i := 0; i+1 < len(out); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(out[i:]))
		value := int(math.Round(float64(sample) * factor))
		if value > math.MaxInt16 {
			value = math.MaxInt16
		} else if value < math.MinInt16 {
			value = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(value)))
	}
	return out
}

func (p *LocalPlayer) fetchAndDecode(ctx context.Context, rawURL string) ([]byte, error) {
	return p.fetchAndDecodeChannel(ctx, rawURL, "mono")
}

func (p *LocalPlayer) fetchAndDecodeChannel(ctx context.Context, rawURL, channel string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create audio request: %w", err)
	}
	response, err := p.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download audio: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download audio: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAudioDownload+1))
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}
	if len(body) > maxAudioDownload {
		return nil, fmt.Errorf("audio exceeds %d bytes", maxAudioDownload)
	}
	contentType := strings.ToLower(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	return decodeAudioChannel(body, contentType, rawURL, channel)
}

func decodeAudio(raw []byte, contentType, rawURL string) ([]byte, error) {
	return decodeAudioChannel(raw, contentType, rawURL, "mono")
}

func decodeAudioChannel(raw []byte, contentType, rawURL, channel string) ([]byte, error) {
	if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WAVE" {
		return decodeWAVChannel(raw, channel)
	}
	ext := ""
	if u, err := url.Parse(rawURL); err == nil {
		ext = strings.ToLower(filepath.Ext(u.Path))
	}
	if contentType == "audio/mpeg" || contentType == "audio/mp3" || ext == ".mp3" || (len(raw) >= 3 && string(raw[:3]) == "ID3") {
		decoder, err := mp3.NewDecoder(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("decode MP3: %w", err)
		}
		stereo, err := io.ReadAll(io.LimitReader(decoder, maxAudioDownload*4))
		if err != nil {
			return nil, fmt.Errorf("read decoded MP3: %w", err)
		}
		mono := selectS16Channel(stereo, 2, channel)
		return resampleS16(mono, decoder.SampleRate(), playbackRate), nil
	}
	return nil, fmt.Errorf("unsupported audio type %q", contentType)
}

func decodeWAV(raw []byte) ([]byte, error) {
	return decodeWAVChannel(raw, "mono")
}

func decodeWAVChannel(raw []byte, channel string) ([]byte, error) {
	var format, channels, rate, bits int
	var data []byte
	for offset := 12; offset+8 <= len(raw); {
		name := string(raw[offset : offset+4])
		rawSize := binary.LittleEndian.Uint32(raw[offset+4 : offset+8])
		offset += 8
		remaining := len(raw) - offset
		size := 0
		streamingData := rawSize == unknownWAVChunkSize && name == "data"
		if streamingData {
			size = remaining
		} else if rawSize == unknownWAVChunkSize || uint64(rawSize) > uint64(remaining) {
			return nil, errors.New("invalid WAV chunk length")
		} else {
			size = int(rawSize)
		}
		chunk := raw[offset : offset+size]
		switch name {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, errors.New("short WAV fmt chunk")
			}
			format = int(binary.LittleEndian.Uint16(chunk[0:2]))
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			rate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
		case "data":
			data = chunk
		}
		if streamingData {
			offset = len(raw)
		} else {
			offset += size + size%2
		}
	}
	if format != 1 || bits != 16 || channels < 1 || channels > 8 || rate < 8000 || rate > 192000 || len(data) == 0 {
		return nil, fmt.Errorf("unsupported WAV format=%d channels=%d rate=%d bits=%d", format, channels, rate, bits)
	}
	mono := selectS16Channel(data, channels, channel)
	return resampleS16(mono, rate, playbackRate), nil
}

func downmixS16(raw []byte, channels int) []int16 {
	return selectS16Channel(raw, channels, "mono")
}

func selectS16Channel(raw []byte, channels int, channel string) []int16 {
	frameBytes := channels * 2
	frames := len(raw) / frameBytes
	out := make([]int16, frames)
	for frame := 0; frame < frames; frame++ {
		if channel == "left" || channel == "right" {
			selected := 0
			if channel == "right" && channels > 1 {
				selected = 1
			}
			offset := frame*frameBytes + selected*2
			out[frame] = int16(binary.LittleEndian.Uint16(raw[offset:]))
			continue
		}
		var sum int64
		for channel := 0; channel < channels; channel++ {
			offset := frame*frameBytes + channel*2
			sum += int64(int16(binary.LittleEndian.Uint16(raw[offset:])))
		}
		out[frame] = int16(sum / int64(channels))
	}
	return out
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func resampleS16(in []int16, sourceRate, targetRate int) []byte {
	if len(in) == 0 || sourceRate <= 0 || targetRate <= 0 {
		return nil
	}
	count := int(math.Ceil(float64(len(in)) * float64(targetRate) / float64(sourceRate)))
	out := make([]byte, count*2)
	if len(in) == 1 {
		for i := 0; i < count; i++ {
			binary.LittleEndian.PutUint16(out[i*2:], uint16(in[0]))
		}
		return out
	}
	ratio := float64(sourceRate) / float64(targetRate)
	for i := 0; i < count; i++ {
		position := float64(i) * ratio
		left := int(position)
		if left >= len(in)-1 {
			left = len(in) - 1
			binary.LittleEndian.PutUint16(out[i*2:], uint16(in[left]))
			continue
		}
		fraction := position - float64(left)
		value := float64(in[left]) + fraction*float64(int(in[left+1])-int(in[left]))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(math.Round(value))))
	}
	return out
}
