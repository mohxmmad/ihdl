package internal

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type SimulationOptions struct {
	DisplayExportPath string
}

type displayViewer struct {
	mu     sync.RWMutex
	frames map[string][]byte
	order  []string
	url    string
}

func renderDisplays(project *Project, circuit *Circuit, env map[string]Value, scope string) error {
	if len(circuit.Displays) == 0 {
		return nil
	}
	if project.Frames == nil {
		project.Frames = make(map[string]DisplayFrame)
	}
	for _, display := range circuit.Displays {
		frame := DisplayFrame{
			Name:   display.Name,
			Scope:  scope,
			Width:  display.Width,
			Height: display.Height,
			Pixels: make([]uint8, display.Width*display.Height*3),
		}
		for _, pixel := range display.Pixels {
			value, ok := env[pixel.Signal]
			if !ok {
				continue
			}
			rgb, err := valueToRGB(value)
			if err != nil {
				return fmt.Errorf("display %s in module %s pixel %d,%d: %w", display.Name, circuit.Name, pixel.X, pixel.Y, err)
			}
			setFramePixel(&frame, pixel.X, pixel.Y, rgb)
		}
		project.Frames[displayFrameKey(scope, display.Name)] = frame
	}
	return nil
}

func valueToRGB(value Value) ([3]uint8, error) {
	switch value.Kind {
	case SignalRGB:
		if len(value.Channels) != 3 {
			return [3]uint8{}, fmt.Errorf("expected rgb pixel")
		}
		return [3]uint8{value.Channels[0], value.Channels[1], value.Channels[2]}, nil
	case SignalBW:
		if len(value.Channels) != 1 {
			return [3]uint8{}, fmt.Errorf("expected bw pixel")
		}
		level := value.Channels[0]
		return [3]uint8{level, level, level}, nil
	default:
		return [3]uint8{}, fmt.Errorf("display inputs must be RGB or BW pixels")
	}
}

func setFramePixel(frame *DisplayFrame, x, y int, rgb [3]uint8) {
	idx := (y*frame.Width + x) * 3
	frame.Pixels[idx] = rgb[0]
	frame.Pixels[idx+1] = rgb[1]
	frame.Pixels[idx+2] = rgb[2]
}

func displayFrameKey(scope, name string) string {
	return scope + "." + name
}

func frameLabel(frame DisplayFrame) string {
	if frame.Scope == "" {
		return frame.Name
	}
	return frame.Scope + "/" + frame.Name
}

func listDisplayFrames(project *Project) []DisplayFrame {
	if len(project.Frames) == 0 {
		return nil
	}
	keys := make([]string, 0, len(project.Frames))
	for key := range project.Frames {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	frames := make([]DisplayFrame, 0, len(keys))
	for _, key := range keys {
		frames = append(frames, project.Frames[key])
	}
	return frames
}

func frameImage(frame DisplayFrame) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	for y := 0; y < frame.Height; y++ {
		for x := 0; x < frame.Width; x++ {
			idx := (y*frame.Width + x) * 3
			img.Set(x, y, color.RGBA{R: frame.Pixels[idx], G: frame.Pixels[idx+1], B: frame.Pixels[idx+2], A: 0xff})
		}
	}
	return img
}

func encodeFramePNG(frame DisplayFrame) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, frameImage(frame)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteDisplayFiles(project *Project, path string) error {
	frames := listDisplayFrames(project)
	if len(frames) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if len(frames) == 1 {
		return writeSingleDisplayFile(frames[0], path)
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, frame := range frames {
		filePath := base + "_" + sanitizeDisplayName(frameLabel(frame)) + ".ippf"
		if err := writeSingleDisplayFile(frame, filePath); err != nil {
			return err
		}
	}
	return nil
}

func writeSingleDisplayFile(frame DisplayFrame, path string) error {
	data, err := encodeFramePNG(frame)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func sanitizeDisplayName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_")
	return replacer.Replace(name)
}

func newDisplayViewer() (*displayViewer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	viewer := &displayViewer{
		frames: make(map[string][]byte),
		url:    "http://" + listener.Addr().String(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", viewer.handleIndex)
	mux.HandleFunc("/frame/", viewer.handleFrame)
	go func() {
		_ = http.Serve(listener, mux)
	}()
	_ = openBrowser(viewer.url)
	return viewer, nil
}

func (viewer *displayViewer) Update(frames []DisplayFrame) error {
	encoded := make(map[string][]byte, len(frames))
	order := make([]string, 0, len(frames))
	for _, frame := range frames {
		key := displayFrameKey(frame.Scope, frame.Name)
		data, err := encodeFramePNG(frame)
		if err != nil {
			return err
		}
		encoded[key] = data
		order = append(order, key)
	}
	sort.Strings(order)
	viewer.mu.Lock()
	viewer.frames = encoded
	viewer.order = order
	viewer.mu.Unlock()
	return nil
}

func (viewer *displayViewer) URL() string {
	return viewer.url
}

func (viewer *displayViewer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	viewer.mu.RLock()
	order := append([]string(nil), viewer.order...)
	viewer.mu.RUnlock()

	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>iHDL Display</title>")
	b.WriteString("<style>body{background:#111;color:#eee;font-family:sans-serif;margin:16px}h1{font-size:18px}section{margin:0 0 24px}img{image-rendering:pixelated;border:1px solid #444;background:#000;max-width:min(80vw,640px);width:100%}p{margin:0 0 8px}</style>")
	b.WriteString("</head><body><h1>iHDL Display</h1>")
	if len(order) == 0 {
		b.WriteString("<p>No display frames yet.</p>")
	} else {
		for _, key := range order {
			b.WriteString("<section><p>")
			b.WriteString(key)
			b.WriteString("</p><img src=\"/frame/")
			b.WriteString(key)
			b.WriteString("?v=\" id=\"")
			b.WriteString(key)
			b.WriteString("\"></section>")
		}
	}
	b.WriteString("<script>setInterval(function(){document.querySelectorAll('img').forEach(function(img){img.src=img.src.split('?')[0]+'?v='+Date.now();});},500);</script>")
	b.WriteString("</body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (viewer *displayViewer) handleFrame(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/frame/")
	viewer.mu.RLock()
	data, ok := viewer.frames[key]
	viewer.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(data)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return nil
	}
	return cmd.Start()
}
