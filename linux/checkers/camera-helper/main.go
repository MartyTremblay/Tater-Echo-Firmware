// This file is copied into the pinned TECHO5 module and built there. Keeping
// the thin HTTP adapter here lets Tater reuse TECHO5's hardware-reviewed
// Checkers camera driver without forking its MediaTek ISP implementation.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
)

const (
	defaultAddress      = "127.0.0.1:43823"
	streamBoundary      = "taterframe"
	streamFramePeriod   = time.Second / 15
	echoStreamPeriod    = time.Second / 20
	streamJPEGQuality   = 60
	echoJPEGQuality     = 55
	snapshotJPEGQuality = 88
)

func main() {
	log.SetOutput(os.Stdout)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	address := strings.TrimSpace(os.Getenv("TATER_CAMERA_ADDRESS"))
	if address == "" {
		address = defaultAddress
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("camera listen: %v", err)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second}
	http.HandleFunc("/health", health)
	http.HandleFunc("/snapshot", snapshot)
	http.HandleFunc("/stream", stream)
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Tater Checkers camera ready on %s", address)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func health(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func snapshot(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	frame, err := camera.Get().Snapshot(ctx)
	if err != nil {
		http.Error(response, err.Error(), http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Content-Type", "image/jpeg")
	response.Header().Set("Cache-Control", "no-store")
	image := frame.Full()
	quality := snapshotJPEGQuality
	profile := request.URL.Query().Get("profile")
	if profile == "video" {
		image = frame.Image()
		quality = streamJPEGQuality
	} else if profile == "echo-low" {
		image = frame.TaterEchoVideo(nil)
		quality = echoJPEGQuality
	}
	if err := jpeg.Encode(response, image, &jpeg.Options{Quality: quality}); err != nil {
		log.Printf("camera JPEG: %v", err)
	}
}

func stream(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		http.Error(response, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	device := camera.Get()
	release, err := device.Acquire()
	if err != nil {
		http.Error(response, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer release()
	frames := make(chan *camera.Frame, 1)
	cancel := device.Frames.Listen(func(frame *camera.Frame) {
		select {
		case frames <- frame:
			return
		default:
		}
		select {
		case <-frames:
		default:
		}
		select {
		case frames <- frame:
		default:
		}
	})
	defer cancel()
	response.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+streamBoundary)
	response.Header().Set("Cache-Control", "no-store, max-age=0")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()
	nextFrame := time.Time{}
	var encoded bytes.Buffer
	encoded.Grow(128 * 1024)
	var echoFrame *image.RGBA
	echoLow := request.URL.Query().Get("profile") == "echo-low"
	statsStarted := time.Now()
	statsFrames := 0
	statsBytes := 0
	var statsEncode time.Duration
	for {
		select {
		case <-request.Context().Done():
			return
		case frame := <-frames:
			now := time.Now()
			if now.Before(nextFrame) {
				continue
			}
			nextFrame = now.Add(cameraStreamPeriod(echoLow))
			encoded.Reset()
			var cameraImage image.Image
			quality := streamJPEGQuality
			if echoLow {
				echoFrame = frame.TaterEchoVideo(echoFrame)
				cameraImage = echoFrame
				quality = echoJPEGQuality
			} else {
				cameraImage = frame.Image()
			}
			encodeStarted := time.Now()
			if err := jpeg.Encode(&encoded, cameraImage, &jpeg.Options{Quality: quality}); err != nil {
				log.Printf("camera stream JPEG: %v", err)
				continue
			}
			statsEncode += time.Since(encodeStarted)
			statsFrames++
			statsBytes += encoded.Len()
			if elapsed := time.Since(statsStarted); elapsed >= 10*time.Second {
				log.Printf("[camera-video] profile=%s %.1f fps, JPEG avg=%s, bytes avg=%d",
					firstNonEmptyProfile(echoLow), float64(statsFrames)/elapsed.Seconds(),
					averageDuration(statsEncode, statsFrames).Round(time.Millisecond), statsBytes/max(statsFrames, 1))
				statsStarted, statsFrames, statsBytes, statsEncode = time.Now(), 0, 0, 0
			}
			if _, err := fmt.Fprintf(response, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", streamBoundary, encoded.Len()); err != nil {
				return
			}
			if _, err := response.Write(encoded.Bytes()); err != nil {
				return
			}
			if _, err := response.Write([]byte("\r\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func firstNonEmptyProfile(echoLow bool) string {
	if echoLow {
		return "echo-low"
	}
	return "video"
}

func averageDuration(total time.Duration, count int) time.Duration {
	if count <= 0 {
		return 0
	}
	return total / time.Duration(count)
}

func cameraStreamPeriod(echoLow bool) time.Duration {
	if echoLow {
		return echoStreamPeriod
	}
	return streamFramePeriod
}
