package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime"
	"sync"

	"github.com/gvcgo/asciinema/asciicast"
	"github.com/gvcgo/asciinema/commands"
	"github.com/gvcgo/asciinema/util"
	"github.com/olivere/ndjson"
)

// 流式写入的结构体
type StreamWriter struct {
	file    *os.File
	writer  *ndjson.Writer
	mu      sync.Mutex
	written bool
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

	return &StreamWriter{
		file:    file,
		writer:  ndjson.NewWriter(file),
		written: true,
	}, nil
}

// 写入帧数据
func (sw *StreamWriter) WriteFrame(frame asciicast.Frame) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	return sw.writer.Encode([]interface{}{frame.Time, "o", string(frame.EventData)})
}

// 关闭文件
func (sw *StreamWriter) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.file != nil {
		return sw.file.Close()
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
		defer streamWriter.Close()

		// 执行流式录制
		cast, err := streamRecorder.ExecuteWithCallback(command, r.Title, r.AssumeYes, r.MaxWait,
			func(frame asciicast.Frame) {
				streamWriter.WriteFrame(frame)
			})

		if err != nil {
			return err
		}
		r.Cast = &cast

		// 流式写入已经完成，修复文件格式
		if err == nil {
			FixCast(r.FilePath)
		}
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
	for _, f := range cast.Stdout {
		if err := result.Encode([]interface{}{f.Time, "o", string(f.EventData)}); err != nil {
			panic(err)
		}
	}

	err = os.WriteFile(r.FilePath, buf.Bytes(), os.ModePerm)
	if err == nil {
		FixCast(r.FilePath)
	}
	return err
}
