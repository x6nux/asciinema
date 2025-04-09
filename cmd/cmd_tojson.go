package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// CommandOutput 表示命令及其输出
type CommandOutput struct {
	Cmd string `json:"cmd"` // 命令
	Out string `json:"out"` // 输出
}

// Frame 表示一个播放帧（内部使用）
type castFrame struct {
	Time      float64 `json:"a"`           // 时间
	EventType string  `json:"b"`           // 事件类型：o（输出）或z（压缩数据）
	EventData []byte  `json:"c"`           // 输出数据
	EndTime   float64 `json:"d,omitempty"` // 压缩帧的结束时间，仅当EventType为z时使用
}

// UnmarshalJSON 自定义JSON反序列化
func (f *castFrame) UnmarshalJSON(data []byte) error {
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

// 是否为压缩帧
func (f *castFrame) IsCompressed() bool {
	return f.EventType == "z"
}

// 处理压缩帧
func processCompressedFrame(frame castFrame) ([]byte, error) {
	// 解码base64数据
	decoded, err := base64.StdEncoding.DecodeString(string(frame.EventData))
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

// ToJSON 将录像文件转换为简化的JSON格式
func (r *Runner) ToJSON() error {
	if r.FilePath == "" {
		return fmt.Errorf("未指定输入文件")
	}

	// 如果没有指定输出文件，则使用与输入文件相同的基础名称，但扩展名为.json
	outputFile := r.FilePath
	if strings.HasSuffix(outputFile, ".cast") {
		outputFile = strings.ReplaceAll(outputFile, ".cast", ".json")
	} else {
		outputFile = outputFile + ".json"
	}

	// 读取录像文件
	data, err := os.ReadFile(r.FilePath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %v", err)
	}

	// 按行分割JSON
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return fmt.Errorf("录像文件格式不正确")
	}

	// 第一行是文件头信息，忽略
	frameLines := lines[1:]

	var commands []CommandOutput
	var currentCommand string
	var currentOutput strings.Builder

	// 创建正则表达式匹配命令行的提示符和输入模式
	cmdPattern := regexp.MustCompile(`^[^\r\n]*[$#>]\s+[^$#>]+$`)

	for _, line := range frameLines {
		if len(line) == 0 {
			continue
		}

		// 解析帧
		var frame castFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			fmt.Printf("解析帧失败: %v, 行: %s\n", err, line)
			continue
		}

		var outputData []byte
		if frame.IsCompressed() {
			// 处理压缩帧
			decompressed, err := processCompressedFrame(frame)
			if err != nil {
				fmt.Printf("处理压缩帧失败: %v\n", err)
				continue
			}
			outputData = decompressed
		} else {
			outputData = frame.EventData
		}

		// 将输出数据转换为字符串
		outputStr := string(outputData)

		// 改进的命令行识别逻辑
		// 1. 检查是否只包含一行以回车结尾的内容
		// 2. 或者匹配常见的命令行模式
		isCommand := false

		// 检查是否是单行命令输入
		if (strings.HasSuffix(outputStr, "\r\n") || strings.HasSuffix(outputStr, "\n")) &&
			strings.Count(outputStr, "\n") <= 1 && strings.Count(outputStr, "\r") <= 1 {
			trimmedStr := strings.TrimRight(outputStr, "\r\n")
			if len(trimmedStr) > 0 && !strings.ContainsAny(trimmedStr, "\r\n") {
				isCommand = true
			}
		}

		// 如果不是单行命令，尝试用正则匹配常见的命令模式
		if !isCommand && cmdPattern.MatchString(strings.TrimRight(outputStr, "\r\n")) {
			isCommand = true
		}

		if isCommand {
			// 如果当前有命令和输出，保存它们
			if currentCommand != "" && currentOutput.Len() > 0 {
				commands = append(commands, CommandOutput{
					Cmd: currentCommand,
					Out: currentOutput.String(),
				})
			}

			// 设置新的当前命令（移除结尾的回车换行）
			currentCommand = strings.TrimRight(outputStr, "\r\n")
			currentOutput.Reset()
		} else {
			// 追加到当前输出
			currentOutput.WriteString(outputStr)
		}
	}

	// 添加最后一个命令及其输出
	if currentCommand != "" && currentOutput.Len() > 0 {
		commands = append(commands, CommandOutput{
			Cmd: currentCommand,
			Out: currentOutput.String(),
		})
	}

	// 如果没有提取到命令，尝试简单处理
	if len(commands) == 0 {
		// 将所有数据分割为可能的命令和输出
		allText := strings.Builder{}
		for _, line := range frameLines {
			if len(line) == 0 {
				continue
			}

			var frame castFrame
			if err := json.Unmarshal([]byte(line), &frame); err != nil {
				continue
			}

			var outputData []byte
			if frame.IsCompressed() {
				decompressed, err := processCompressedFrame(frame)
				if err != nil {
					continue
				}
				outputData = decompressed
			} else {
				outputData = frame.EventData
			}

			allText.Write(outputData)
		}

		// 分割所有文本为行
		allLines := strings.Split(allText.String(), "\n")
		var cmd, out strings.Builder

		for i, line := range allLines {
			line = strings.TrimRight(line, "\r")
			if i == 0 && len(line) > 0 {
				// 第一行作为命令
				cmd.WriteString(line)
			} else if len(line) > 0 {
				// 其他行作为输出
				out.WriteString(line)
				out.WriteString("\n")
			}
		}

		if cmd.Len() > 0 && out.Len() > 0 {
			commands = append(commands, CommandOutput{
				Cmd: cmd.String(),
				Out: out.String(),
			})
		}
	}

	// 将结果写入JSON文件
	resultJSON, err := json.Marshal(commands)
	if err != nil {
		return fmt.Errorf("生成JSON失败: %v", err)
	}

	if err := os.WriteFile(outputFile, resultJSON, 0644); err != nil {
		return fmt.Errorf("写入JSON文件失败: %v", err)
	}

	fmt.Printf("转换成功，输出文件: %s\n", outputFile)
	return nil
}
