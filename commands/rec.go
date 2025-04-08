package commands

import (
	"github.com/x6nux/asciinema/asciicast"
)

type RecordCommand struct {
	Env      map[string]string
	Recorder asciicast.Recorder
}

// 支持流式写入的录制命令
type StreamRecordCommand struct {
	Env      map[string]string
	Recorder asciicast.Recorder
}

func NewRecordCommand(env map[string]string) *RecordCommand {
	return &RecordCommand{
		Env:      env,
		Recorder: asciicast.NewRecorder(),
	}
}

func NewStreamRecordCommand(env map[string]string) *StreamRecordCommand {
	return &StreamRecordCommand{
		Env:      env,
		Recorder: asciicast.NewRecorder(),
	}
}

func (c *RecordCommand) Execute(command, title string, assumeYes bool, maxWait float64) (asciicast.Asciicast, error) {
	return c.Recorder.Record(command, title, maxWait, assumeYes, c.Env)
}

// 支持回调的录制方法
func (c *StreamRecordCommand) ExecuteWithCallback(command, title string, assumeYes bool, maxWait float64, callback asciicast.FrameCallback) (asciicast.Asciicast, error) {
	// 调用支持回调的录制方法
	return c.Recorder.RecordWithCallback(command, title, maxWait, assumeYes, c.Env, callback)
}

// 获取终端大小的辅助方法
func (c *StreamRecordCommand) GetTerminalSize() (rows, cols int, err error) {
	return c.Recorder.GetTerminalSize()
}
