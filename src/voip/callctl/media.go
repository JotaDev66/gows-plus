package callctl

import (
	"os"
	"sync"
	"time"

	"github.com/devlikeapro/gows/voip/call"
	"github.com/devlikeapro/gows/voip/media"
)

// mlowSampleRate is the sample rate the MLow codec (and WhatsApp voice calls)
// operate at. The headless bridge assumes uplink/downlink PCM at this rate,
// mono, signed 16-bit little-endian.
const mlowSampleRate = 16000

// FileAudioSource streams raw 16 kHz mono s16le PCM from a file into a call's
// uplink. It paces playback in real time, feeding one codec frame per frame
// interval so the CallManager send loop always has fresh audio.
type FileAudioSource struct {
	path string
	stop chan struct{}
	once sync.Once
}

// NewFileAudioSource builds a source that reads raw s16le 16 kHz mono PCM.
func NewFileAudioSource(path string) *FileAudioSource {
	return &FileAudioSource{path: path, stop: make(chan struct{})}
}

// Start loads the PCM file and feeds frameSize samples every frame interval into
// the manager. It returns an error if the file cannot be read; the streaming
// itself runs in a background goroutine.
func (s *FileAudioSource) Start(cm *call.CallManager, frameSize int) error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	pcm := media.PCMInt16LEToFloat32(data)
	interval := time.Duration(frameSize) * time.Second / time.Duration(mlowSampleRate)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		pos := 0
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
			}
			if pos >= len(pcm) {
				return
			}
			end := pos + frameSize
			if end > len(pcm) {
				end = len(pcm)
			}
			cm.FeedCapturedPCM(pcm[pos:end])
			pos = end
		}
	}()
	return nil
}

// Stop halts the streaming goroutine. It is safe to call more than once.
func (s *FileAudioSource) Stop() {
	s.once.Do(func() { close(s.stop) })
}

// FileAudioSink writes peer (downlink) audio to a file as raw 16 kHz mono s16le
// PCM. Callers can post-process it into WAV/OGG with any standard tool.
type FileAudioSink struct {
	mu sync.Mutex
	f  *os.File
}

// NewFileAudioSink creates (or truncates) the output PCM file.
func NewFileAudioSink(path string) (*FileAudioSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &FileAudioSink{f: f}, nil
}

// WritePCM appends 16 kHz mono float32 PCM as s16le. It is a no-op on a closed
// or nil sink.
func (s *FileAudioSink) WritePCM(pcm []float32) error {
	if s == nil || len(pcm) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	_, err := s.f.Write(media.PCMFloat32ToInt16LE(pcm))
	return err
}

// Close flushes and closes the output file. It is safe to call more than once.
func (s *FileAudioSink) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}
