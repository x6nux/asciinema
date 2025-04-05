package asciicast

import (
	"sync"
	"time"
)

type Stream struct {
	Frames        []Frame
	elapsedTime   time.Duration
	lastWriteTime time.Time
	maxWait       time.Duration
	lock          *sync.Mutex
	callback      func(frame Frame)
}

func NewStream(maxWait float64) *Stream {
	if maxWait <= 0 {
		maxWait = 1.0
	}
	return &Stream{
		lastWriteTime: time.Now(),
		maxWait:       time.Duration(maxWait*1000000) * time.Microsecond,
		lock:          &sync.Mutex{},
	}
}

// 创建支持回调的Stream
func NewStreamWithCallback(maxWait float64, callback func(frame Frame)) *Stream {
	if maxWait <= 0 {
		maxWait = 1.0
	}
	return &Stream{
		lastWriteTime: time.Now(),
		maxWait:       time.Duration(maxWait*1000000) * time.Microsecond,
		lock:          &sync.Mutex{},
		callback:      callback,
	}
}

func (s *Stream) Write(p []byte) (int, error) {
	frame := Frame{}
	frame.EventType = "o"
	frame.Time = s.incrementElapsedTime().Seconds()
	frame.EventData = make([]byte, len(p))
	copy(frame.EventData, p)
	s.Frames = append(s.Frames, frame)

	// 如果有回调函数，实时调用回调处理帧数据
	if s.callback != nil {
		s.callback(frame)
	}

	return len(p), nil
}

func (s *Stream) Close() {
	s.incrementElapsedTime()

	if len(s.Frames) > 0 && string(s.Frames[len(s.Frames)-1].EventData) == "exit\r\n" {
		s.Frames = s.Frames[:len(s.Frames)-1]
	}
}

func (s *Stream) Duration() time.Duration {
	return s.elapsedTime
}

func (s *Stream) incrementElapsedTime() time.Duration {
	s.lock.Lock()
	now := time.Now()
	d := now.Sub(s.lastWriteTime)

	if s.maxWait > 0 && d > s.maxWait {
		d = s.maxWait
	}
	if d <= 0 {
		d = time.Millisecond * 500
	}

	s.elapsedTime += d
	s.lastWriteTime = now
	s.lock.Unlock()
	return s.elapsedTime
}
