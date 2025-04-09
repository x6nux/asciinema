package terminal

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"time"
)

// 自定义本地类型，避免循环引用asciicast包
type Cast interface {
	GetWidth() int
	GetHeight() int
	GetFrames() interface{}
}

type Frame interface {
	GetTime() float64
	GetEventType() string
	GetEventData() []byte
	IsCompressed() bool
}

// Player 是播放器接口
type Player interface {
	Play(cast Cast, speed float64) error
}

// AsciicastPlayer 实现了Player接口
type AsciicastPlayer struct {
	Terminal Terminal
}

func NewPlayer() Player {
	return &AsciicastPlayer{
		Terminal: NewTerminal(),
	}
}

// processCompressedFrame 处理新版z型压缩帧，解码并解压缩数据
func (p *AsciicastPlayer) processCompressedFrame(frame Frame) ([]byte, error) {
	// 解码base64数据
	decoded, err := base64.StdEncoding.DecodeString(string(frame.GetEventData()))
	if err != nil {
		return nil, fmt.Errorf("base64解码失败: %v", err)
	}

	// 使用gzip解压数据
	gzipReader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("创建gzip读取器失败: %v", err)
	}
	defer gzipReader.Close()

	// 读取解压后的数据
	decompressed, err := io.ReadAll(gzipReader)
	if err != nil {
		return nil, fmt.Errorf("读取解压数据失败: %v", err)
	}

	return decompressed, nil
}

// Play 播放一个ASCII录屏
func (r *AsciicastPlayer) Play(cast Cast, speed float64) error {
	// 调整终端大小 - 不使用SetSize，因为Terminal接口没有这个方法
	// 这里只获取终端大小信息，不进行调整
	if cast.GetWidth() > 0 && cast.GetHeight() > 0 {
		// 注意：此处不再尝试调整终端大小
		// r.Terminal.SetSize(cast.GetWidth(), cast.GetHeight())
	}

	// 设置初始时间
	var timeAdjustment time.Duration

	// 尝试不同的类型断言获取帧数据
	var frames []Frame

	// 1. 尝试直接转换为Frame切片
	if framesTyped, ok := cast.GetFrames().([]Frame); ok {
		frames = framesTyped
	} else if framesProvider, ok := cast.(interface{ Frames() []interface{} }); ok {
		// 2. 尝试通过Frames()方法获取
		rawFrames := framesProvider.Frames()
		frames = make([]Frame, len(rawFrames))
		for i, f := range rawFrames {
			if frame, ok := f.(Frame); ok {
				frames[i] = frame
			} else {
				return fmt.Errorf("帧 #%d 不是有效的Frame类型", i)
			}
		}
	} else {
		// 无法获取帧数据
		return fmt.Errorf("不支持的帧类型")
	}

	// 遍历所有帧
	for i, frame := range frames {
		var sleepTime time.Duration

		// 计算等待时间
		if i > 0 {
			// 使用当前帧与前一帧的时间差作为等待时间
			delay := frame.GetTime() - frames[i-1].GetTime()
			if delay < 0 {
				delay = 0
			}
			sleepTime = time.Duration(float64(delay)*1000/speed) * time.Millisecond
		}

		// 等待相应时间（减去处理前一帧所用的时间）
		if sleepTime > timeAdjustment {
			time.Sleep(sleepTime - timeAdjustment)
		}

		startTime := time.Now()

		// 处理帧数据
		var data []byte
		var err error

		// 根据帧类型进行处理
		if frame.IsCompressed() {
			// 处理压缩帧
			data, err = r.processCompressedFrame(frame)
			if err != nil {
				log.Printf("处理压缩帧失败: %v", err)
				continue
			}
		} else {
			// 处理普通输出帧
			data = frame.GetEventData()
		}

		// 输出到终端 - Terminal.Write只返回error，不是标准的(int, error)
		err = r.Terminal.Write(data)
		if err != nil {
			return err
		}

		// 计算处理这一帧所花费的时间，用于下一帧的等待时间调整
		timeAdjustment = time.Since(startTime)
	}

	return nil
}
