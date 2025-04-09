package asciicast

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Frame 表示一个播放帧
type Frame struct {
	Time      float64 `json:"a"`           // 时间
	EventType string  `json:"b"`           // 事件类型：o（输出）或z（压缩数据）
	EventData []byte  `json:"c"`           // 输出数据
	EndTime   float64 `json:"d,omitempty"` // 压缩帧的结束时间，仅当EventType为z时使用
}

// MarshalJSON 自定义JSON序列化，以适应asciicast v2格式
func (f Frame) MarshalJSON() ([]byte, error) {
	// 特殊处理z类型压缩帧
	if f.EventType == "z" {
		// 使用结构体序列化
		type ZFrame struct {
			Time      float64 `json:"a"`
			EventType string  `json:"b"`
			EventData string  `json:"c"`
			EndTime   float64 `json:"d,omitempty"`
		}

		zf := ZFrame{
			Time:      f.Time,
			EventType: f.EventType,
			EventData: string(f.EventData), // 压缩帧的数据已经是base64编码的
			EndTime:   f.EndTime,
		}

		return json.Marshal(zf)
	}

	// 普通输出帧，格式为[time, type, data]
	return json.Marshal([]interface{}{f.Time, f.EventType, string(f.EventData)})
}

// UnmarshalJSON 自定义JSON反序列化，以支持不同格式
func (f *Frame) UnmarshalJSON(data []byte) error {
	// 检查是结构体还是数组格式
	if bytes.HasPrefix(data, []byte("{")) {
		// 结构体格式，可能是压缩帧
		var frameMap map[string]interface{}
		if err := json.Unmarshal(data, &frameMap); err != nil {
			return err
		}

		// 提取基本属性
		if time, ok := frameMap["a"].(float64); ok {
			f.Time = time
		}

		if eventType, ok := frameMap["b"].(string); ok {
			f.EventType = eventType
		}

		if eventData, ok := frameMap["c"].(string); ok {
			f.EventData = []byte(eventData)
		}

		if endTime, ok := frameMap["d"].(float64); ok {
			f.EndTime = endTime
		}

		return nil
	}

	// 数组格式，标准的asciicast v2帧: [time, type, data]
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}

	if len(arr) < 3 {
		return fmt.Errorf("invalid frame format: %s", string(data))
	}

	// 提取时间
	if time, ok := arr[0].(float64); ok {
		f.Time = time
	} else {
		return fmt.Errorf("invalid time format: %v", arr[0])
	}

	// 提取类型
	if eventType, ok := arr[1].(string); ok {
		f.EventType = eventType
	} else {
		return fmt.Errorf("invalid event type: %v", arr[1])
	}

	// 提取数据
	if eventData, ok := arr[2].(string); ok {
		f.EventData = []byte(eventData)
	} else {
		return fmt.Errorf("invalid event data: %v", arr[2])
	}

	return nil
}

// IsCompressed 检查帧是否为压缩帧
func (f *Frame) IsCompressed() bool {
	return f.EventType == "z"
}

// CompressFrameData 使用gzip和base64压缩帧数据
func CompressFrameData(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	// 创建gzip压缩器
	gzipWriter := gzip.NewWriter(&buf)

	// 写入数据
	if _, err := gzipWriter.Write(data); err != nil {
		return nil, fmt.Errorf("压缩数据失败: %v", err)
	}

	// 关闭压缩器
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("关闭压缩器失败: %v", err)
	}

	// 对压缩后的数据进行base64编码
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return []byte(encoded), nil
}

// NewCompressedFrame 创建一个新的压缩帧
func NewCompressedFrame(startTime, endTime float64, data []byte) (*Frame, error) {
	compressedData, err := CompressFrameData(data)
	if err != nil {
		return nil, err
	}

	return &Frame{
		Time:      startTime,
		EventType: "z",
		EventData: compressedData,
		EndTime:   endTime,
	}, nil
}
