package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/olivere/ndjson"
	"github.com/x6nux/asciinema/asciicast"
	"github.com/x6nux/asciinema/commands"
	"github.com/x6nux/asciinema/util"
)

// 获取当前时间的毫秒值
func currentTimeMs() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

// 流式写入的结构体
type StreamWriter struct {
	file           *os.File
	writer         *ndjson.Writer
	mu             sync.Mutex
	written        bool
	filePath       string
	lastSyncTime   int64             // 上次同步时间（毫秒）
	syncIntervalMs int64             // 同步间隔（毫秒）
	enableCompress bool              // 是否启用压缩
	compressRatio  int               // 压缩比例，值越大压缩效果越明显但可能影响回放质量
	batchFrames    []asciicast.Frame // 用于批量压缩的帧缓冲区
	batchSize      int               // 批处理大小
	lastWriteTime  float64           // 上次写入的时间点
	totalDataSize  int               // 当前批次累积的数据大小
	minBatchSize   int               // 最小批处理大小
	maxBatchSize   int               // 最大批处理大小
	dataThreshold  int               // 数据大小阈值，超过此值将触发压缩
}

// 创建新的流式写入器
func NewStreamWriter(filepath string, header *asciicast.Header) (*StreamWriter, error) {
	file, err := os.Create(filepath)
	if err != nil {
		return nil, err
	}

	// 写入头部信息
	enc := json.NewEncoder(file)
	if err := enc.Encode(header); err != nil {
		file.Close()
		return nil, err
	}

	// 设置文件缓冲区以减少写入操作数量
	return &StreamWriter{
		file:           file,
		writer:         ndjson.NewWriter(file),
		written:        true,
		filePath:       filepath,
		lastSyncTime:   0,
		syncIntervalMs: 500,                            // 默认500毫秒同步一次
		enableCompress: true,                           // 默认启用压缩
		compressRatio:  8,                              // 默认压缩比例为8，此处表示批处理大小
		batchFrames:    make([]asciicast.Frame, 0, 32), // 初始化批处理缓冲区
		batchSize:      8,                              // 默认批处理大小
		lastWriteTime:  0,
		totalDataSize:  0,
		minBatchSize:   4,    // 最小批处理大小
		maxBatchSize:   32,   // 最大批处理大小
		dataThreshold:  4096, // 4KB数据大小阈值
	}, nil
}

// 使用gzip压缩数据
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)

	if _, err := gzipWriter.Write(data); err != nil {
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// 检查两帧内容的相似度
func contentSimilarity(a, b []byte) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// 比较前min(len(a), len(b), 32)个字节的相同比例作为相似度指标
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	if minLen > 32 {
		minLen = 32 // 只比较前32个字节，提高效率
	}

	sameCount := 0
	for i := 0; i < minLen; i++ {
		if a[i] == b[i] {
			sameCount++
		}
	}

	return float64(sameCount) / float64(minLen)
}

// 批量压缩并写入帧数据
func (sw *StreamWriter) flushBatchFrames() error {
	if len(sw.batchFrames) == 0 {
		return nil
	}

	// 如果不启用压缩或帧数太少，直接写入
	if !sw.enableCompress || len(sw.batchFrames) < sw.minBatchSize {
		for _, frame := range sw.batchFrames {
			if err := sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)}); err != nil {
				return err
			}
		}
		sw.batchFrames = sw.batchFrames[:0] // 清空批处理缓冲区
		sw.totalDataSize = 0
		return nil
	}

	// 基于内容相似度进行智能分组
	groups := make([][]asciicast.Frame, 0)
	currentGroup := []asciicast.Frame{sw.batchFrames[0]}
	totalGroups := 1

	for i := 1; i < len(sw.batchFrames); i++ {
		// 检查时间连续性和内容相似度
		timeDiff := sw.batchFrames[i].Time - sw.batchFrames[i-1].Time
		contentSim := contentSimilarity(sw.batchFrames[i].EventData, sw.batchFrames[i-1].EventData)

		// 如果时间接近且内容相似度高，则加入当前组
		if timeDiff < 1.0 && contentSim > 0.5 {
			currentGroup = append(currentGroup, sw.batchFrames[i])
		} else {
			// 否则，创建新组
			if len(currentGroup) > 0 {
				groups = append(groups, currentGroup)
			}
			currentGroup = []asciicast.Frame{sw.batchFrames[i]}
			totalGroups++
		}
	}

	// 添加最后一组
	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	// 如果分组导致单个组太小，则合并小组
	if totalGroups > 1 {
		optimizedGroups := make([][]asciicast.Frame, 0)
		currentMergedGroup := make([]asciicast.Frame, 0)

		for _, group := range groups {
			// 如果当前合并组加上新组的大小小于最大批处理大小，则合并
			if len(currentMergedGroup)+len(group) <= sw.maxBatchSize {
				currentMergedGroup = append(currentMergedGroup, group...)
			} else {
				// 如果合并后超过最大批处理大小，则先保存当前合并组
				if len(currentMergedGroup) > 0 {
					optimizedGroups = append(optimizedGroups, currentMergedGroup)
				}
				// 检查新组的大小
				if len(group) >= sw.minBatchSize {
					optimizedGroups = append(optimizedGroups, group)
					currentMergedGroup = make([]asciicast.Frame, 0)
				} else {
					currentMergedGroup = group
				}
			}
		}

		// 添加最后一个合并组
		if len(currentMergedGroup) > 0 {
			optimizedGroups = append(optimizedGroups, currentMergedGroup)
		}

		groups = optimizedGroups
	}

	// 为每个组应用压缩
	for _, group := range groups {
		// 对于非常小的组，直接写入不压缩
		if len(group) < sw.minBatchSize {
			for _, frame := range group {
				if err := sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)}); err != nil {
					return err
				}
			}
			continue
		}

		// 获取分组的起始和结束时间
		startTime := group[0].Time
		endTime := group[len(group)-1].Time

		// 合并组内所有帧的数据用于压缩
		var allFramesData bytes.Buffer

		for _, frame := range group {
			allFramesData.Write(frame.EventData)
		}

		// 压缩合并后的数据
		compressedData, err := compressData(allFramesData.Bytes())
		if err != nil {
			// 压缩失败，降级为普通写入
			for _, frame := range group {
				if err := sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)}); err != nil {
					return err
				}
			}
			continue
		}

		// 计算压缩比，数据量很小时容忍较低的压缩比
		compressionRatio := float64(len(compressedData)) / float64(allFramesData.Len())
		// 根据数据大小动态调整压缩阈值
		compressionThreshold := 0.95
		if allFramesData.Len() > 1024 {
			compressionThreshold = 0.9
		}
		if allFramesData.Len() > 8192 {
			compressionThreshold = 0.85
		}

		if compressionRatio < compressionThreshold {
			// 将压缩数据编码为base64以确保兼容性
			encoded := base64.StdEncoding.EncodeToString(compressedData)

			// 创建一个专门的压缩帧，包含起始和结束时间
			compressFrame := asciicast.Frame{
				Time:      startTime,
				EndTime:   endTime,
				EventType: "z",
				EventData: []byte(encoded),
			}

			// 直接将Frame序列化为JSON并写入文件
			encodedFrame, err := json.Marshal(compressFrame)
			if err != nil {
				// JSON编码失败，降级为普通写入
				for _, frame := range group {
					if err := sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)}); err != nil {
						return err
					}
				}
			} else {
				// 写入压缩帧并添加换行符
				if _, err := sw.file.Write(encodedFrame); err != nil {
					return err
				}
				if _, err := sw.file.Write([]byte("\n")); err != nil {
					return err
				}
			}
		} else {
			// 压缩效果不好，使用原始数据
			for _, frame := range group {
				if err := sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)}); err != nil {
					return err
				}
			}
		}
	}

	sw.batchFrames = sw.batchFrames[:0] // 清空批处理缓冲区
	sw.totalDataSize = 0
	return nil
}

// 写入帧数据
func (sw *StreamWriter) WriteFrame(frame asciicast.Frame) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// 如果启用压缩，则将帧添加到批处理缓冲区
	if sw.enableCompress {
		// 计算当前帧数据大小
		currentFrameSize := len(frame.EventData)

		// 如果时间差大于阈值或累积数据大小超过阈值，先刷新缓冲区
		if (frame.Time-sw.lastWriteTime > 1.5 && len(sw.batchFrames) >= sw.minBatchSize) ||
			(sw.totalDataSize > sw.dataThreshold && len(sw.batchFrames) >= sw.minBatchSize) {
			if err := sw.flushBatchFrames(); err != nil {
				return err
			}
		}

		// 添加当前帧到批处理缓冲区
		sw.batchFrames = append(sw.batchFrames, frame)
		sw.lastWriteTime = frame.Time
		sw.totalDataSize += currentFrameSize

		// 如果缓冲区达到最大批处理大小，刷新缓冲区
		if len(sw.batchFrames) >= sw.maxBatchSize {
			if err := sw.flushBatchFrames(); err != nil {
				return err
			}
		}
	} else {
		// 不启用压缩，直接写入
		if err := sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)}); err != nil {
			return err
		}
	}

	// 基于时间的同步策略，减少file.Sync()调用频率
	currentTime := currentTimeMs()
	if currentTime-sw.lastSyncTime >= sw.syncIntervalMs {
		sw.file.Sync()
		sw.lastSyncTime = currentTime
	}

	return nil
}

// 关闭文件
func (sw *StreamWriter) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.file != nil {
		// 刷新剩余的批处理帧
		if sw.enableCompress && len(sw.batchFrames) > 0 {
			sw.flushBatchFrames()
		}

		// 最后一次刷新确保所有数据写入磁盘
		sw.file.Sync()
		err := sw.file.Close()
		// 关闭后立即修复文件格式
		FixCast(sw.filePath)
		return err
	}
	return nil
}

func (r *Runner) Rec() error {
	command := "C:\\WINDOWS\\System32\\WindowsPowerShell\\v1.0\\powershell.exe"
	if ok, _ := util.PathIsExist(command); !ok {
		command = "powershell.exe"
	}
	if runtime.GOOS != "windows" {
		command = util.FirstNonBlank(os.Getenv("SHELL"), cfg.RecordCommand())
	}

	if r.Quite {
		util.BeQuiet()
		r.AssumeYes = true
	}

	cmd := commands.NewRecordCommand(env)

	// 如果开启流式写入，需要修改Recorder接口以支持回调
	if r.StreamWrite {
		// 创建自定义的StreamRecorder
		streamRecorder := commands.NewStreamRecordCommand(env)

		// 构建header
		rows, cols, _ := streamRecorder.Recorder.GetTerminalSize()
		header := &asciicast.Header{
			Version:   2,
			Command:   command,
			Title:     r.Title,
			Width:     cols,
			Height:    rows,
			Timestamp: 0, // 会在实际录制开始时更新
		}

		// 创建流式写入器
		streamWriter, err := NewStreamWriter(r.FilePath, header)
		if err != nil {
			return err
		}

		// 如果设置了同步间隔，则更新
		if r.SyncInterval > 0 {
			streamWriter.syncIntervalMs = r.SyncInterval
		}

		// 如果设置了不压缩，则禁用压缩
		if r.DisableCompress {
			streamWriter.enableCompress = false
		}

		// 如果设置了压缩比例，则更新
		if r.CompressRatio > 0 {
			streamWriter.compressRatio = r.CompressRatio
			streamWriter.batchSize = r.CompressRatio // 使用压缩比例作为批处理大小
		}

		// 使用defer确保无论如何退出都会关闭文件并修复格式
		defer func() {
			streamWriter.Close()
			// 再次修复文件格式，以应对任何情况
			FixCast(r.FilePath)
		}()

		// 设置信号处理，捕获退出信号以修复文件格式
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

		// 创建一个完成通道，用于正常退出时的信号
		done := make(chan struct{})

		go func() {
			select {
			case <-signalChan:
				// 信号退出
				streamWriter.Close()
				os.Exit(0)
			case <-done:
				// 正常退出，不做任何事
				return
			}
		}()

		// 执行流式录制
		cast, err := streamRecorder.ExecuteWithCallback(command, r.Title, r.AssumeYes, r.MaxWait,
			func(frame asciicast.Frame) {
				// 捕获写入过程中的任何可能异常
				defer func() {
					if r := recover(); r != nil {
						// 如果写入过程中panic，确保文件被修复
						streamWriter.Close()
						FixCast(streamWriter.filePath)
					}
				}()

				streamWriter.WriteFrame(frame)
			})

		// 通知信号处理协程已完成
		close(done)

		if err != nil {
			return err
		}
		r.Cast = &cast

		// 流式写入已经完成，修复文件格式
		FixCast(r.FilePath)

		return err
	}

	// 传统模式：先全部录制，然后一次性写入文件
	cast, err := cmd.Execute(command, r.Title, r.AssumeYes, r.MaxWait)
	if err != nil {
		return err
	}
	r.Cast = &cast
	var buf bytes.Buffer
	result := ndjson.NewWriter(&buf)

	header := &asciicast.Header{
		Version:   cast.Version,
		Command:   cast.Command,
		Title:     cast.Title,
		Width:     cast.Width,
		Height:    cast.Height,
		Timestamp: cast.Timestamp,
		Duration:  cast.Duration,
		Env:       cast.Env,
	}

	// add header
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(&header); err != nil {
		return err
	}

	// 如果启用了压缩功能，执行批量压缩
	if !r.DisableCompress && len(cast.Stdout) > 0 {
		// 确定最小、最大和目标批处理大小
		minBatchSize := 4
		maxBatchSize := 64
		targetBatchSize := 16
		if r.CompressRatio > 0 {
			targetBatchSize = r.CompressRatio
			if targetBatchSize < minBatchSize {
				targetBatchSize = minBatchSize
			}
			if targetBatchSize > maxBatchSize {
				targetBatchSize = maxBatchSize
			}
		}

		// 对帧进行智能分组
		groups := make([][]asciicast.Frame, 0)
		if len(cast.Stdout) > 0 {
			currentGroup := []asciicast.Frame{cast.Stdout[0]}

			for i := 1; i < len(cast.Stdout); i++ {
				// 检查时间连续性和内容相似度
				timeDiff := cast.Stdout[i].Time - cast.Stdout[i-1].Time
				contentSim := contentSimilarity(cast.Stdout[i].EventData, cast.Stdout[i-1].EventData)

				// 当前组大小控制
				currentGroupSize := 0
				for _, f := range currentGroup {
					currentGroupSize += len(f.EventData)
				}

				// 如果时间接近且内容相似度高，且组大小未超限，则加入当前组
				if timeDiff < 1.0 && contentSim > 0.5 && len(currentGroup) < maxBatchSize && currentGroupSize < 32768 {
					currentGroup = append(currentGroup, cast.Stdout[i])
				} else {
					// 如果当前组小于最小大小但时间差不大，尝试继续添加
					if len(currentGroup) < minBatchSize && timeDiff < 0.5 {
						currentGroup = append(currentGroup, cast.Stdout[i])
					} else {
						// 否则，创建新组
						if len(currentGroup) > 0 {
							groups = append(groups, currentGroup)
						}
						currentGroup = []asciicast.Frame{cast.Stdout[i]}
					}
				}
			}

			// 添加最后一组
			if len(currentGroup) > 0 {
				groups = append(groups, currentGroup)
			}

			// 合并小组
			if len(groups) > 1 {
				optimizedGroups := make([][]asciicast.Frame, 0)
				currentMergedGroup := make([]asciicast.Frame, 0)

				for _, group := range groups {
					// 如果当前合并组加上新组的大小适中，则合并
					if len(currentMergedGroup)+len(group) <= targetBatchSize*2 {
						currentMergedGroup = append(currentMergedGroup, group...)
					} else {
						// 如果合并后超过目标大小的2倍，则先保存当前合并组
						if len(currentMergedGroup) > 0 {
							optimizedGroups = append(optimizedGroups, currentMergedGroup)
						}
						// 检查新组的大小
						if len(group) >= minBatchSize {
							optimizedGroups = append(optimizedGroups, group)
							currentMergedGroup = make([]asciicast.Frame, 0)
						} else {
							currentMergedGroup = group
						}
					}
				}

				// 添加最后一个合并组
				if len(currentMergedGroup) > 0 {
					optimizedGroups = append(optimizedGroups, currentMergedGroup)
				}

				groups = optimizedGroups
			}
		}

		// 为每个组应用压缩
		for _, group := range groups {
			// 对于非常小的组，直接写入不压缩
			if len(group) < minBatchSize {
				for _, f := range group {
					if err := result.Encode([]interface{}{f.Time, "o", string(f.EventData)}); err != nil {
						return err
					}
				}
				continue
			}

			// 获取分组的起始和结束时间
			startTime := group[0].Time
			endTime := group[len(group)-1].Time

			// 合并组内所有帧的数据用于压缩
			var allFramesData bytes.Buffer

			for _, frame := range group {
				allFramesData.Write(frame.EventData)
			}

			// 压缩合并后的数据
			compressedData, err := compressData(allFramesData.Bytes())
			if err != nil {
				// 压缩失败，降级为普通写入
				for _, f := range group {
					if err := result.Encode([]interface{}{f.Time, "o", string(f.EventData)}); err != nil {
						return err
					}
				}
				continue
			}

			// 计算压缩比，根据数据大小动态调整阈值
			compressionRatio := float64(len(compressedData)) / float64(allFramesData.Len())
			compressionThreshold := 0.95
			if allFramesData.Len() > 1024 {
				compressionThreshold = 0.9
			}
			if allFramesData.Len() > 8192 {
				compressionThreshold = 0.85
			}

			if compressionRatio < compressionThreshold {
				// 将压缩数据编码为base64以确保兼容性
				encoded := base64.StdEncoding.EncodeToString(compressedData)

				// 创建一个专门的压缩帧，包含起始和结束时间
				compressFrame := asciicast.Frame{
					Time:      startTime,
					EndTime:   endTime,
					EventType: "z",
					EventData: []byte(encoded),
				}

				// 序列化压缩帧并写入缓冲区
				compressFrameJSON, err := json.Marshal(compressFrame)
				if err != nil {
					// JSON编码失败，降级为普通写入
					for _, f := range group {
						if err := result.Encode([]interface{}{f.Time, "o", string(f.EventData)}); err != nil {
							return err
						}
					}
				} else {
					// 写入压缩帧并添加换行符
					buf.Write(compressFrameJSON)
					buf.Write([]byte("\n"))
				}
			} else {
				// 压缩效果不好，使用原始数据
				for _, f := range group {
					if err := result.Encode([]interface{}{f.Time, "o", string(f.EventData)}); err != nil {
						return err
					}
				}
			}
		}
	} else {
		// 不压缩，直接写入所有帧
		for _, f := range cast.Stdout {
			if err := result.Encode([]interface{}{f.Time, "o", string(f.EventData)}); err != nil {
				panic(err)
			}
		}
	}

	err = os.WriteFile(r.FilePath, buf.Bytes(), os.ModePerm)
	if err == nil {
		FixCast(r.FilePath)
	}
	return err
}
