package asciicast

import (
	"os"

	"github.com/x6nux/asciinema/terminal"
	"github.com/x6nux/asciinema/util"
)

const (
	warnCols = 120
	warnRows = 30
)

// FrameCallback 定义帧处理的回调函数类型
type FrameCallback func(frame Frame)

type Recorder interface {
	Record(string, string, float64, bool, map[string]string) (Asciicast, error)
	// 添加支持回调的录制方法
	RecordWithCallback(command, title string, maxWait float64, assumeYes bool, env map[string]string, callback FrameCallback) (Asciicast, error)
	// 获取终端大小
	GetTerminalSize() (rows, cols int, err error)
}

type AsciicastRecorder struct {
	Terminal terminal.Terminal
}

func NewRecorder() Recorder {
	return &AsciicastRecorder{Terminal: terminal.NewTerminal()}
}

func (r *AsciicastRecorder) Record(command, title string, maxWait float64, assumeYes bool, env map[string]string) (Asciicast, error) {
	rows, cols, _ := r.Terminal.Size()
	if rows > warnRows || cols > warnCols {
		if !assumeYes {
			doneChan := r.checkTerminalSize()
			util.Warningf("Current terminal size is %vx%v.", cols, rows)
			util.Warningf("It may be too big to be properly replayed on smaller screens.")
			util.Warningf("You can now resize it. Press <Enter> to start recording.")
			util.ReadLine()
			doneChan <- true
		}
	}
	os.Setenv("ASCIINEMA_RECORDING", "true")
	util.Printf("Asciicast recording started.")
	util.Printf(`Hit Ctrl-D or type "exit" to finish.`)

	stdout := NewStream(maxWait)

	err := r.Terminal.Record(command, stdout)
	if err != nil {
		return Asciicast{}, err
	}

	stdout.Close()

	util.Printf("Asciicast recording finished.")

	rows, cols, _ = r.Terminal.Size()

	asciicast := NewAsciicast(
		cols,
		rows,
		stdout.Duration().Seconds(),
		command,
		title,
		stdout.Frames,
		env,
	)

	os.Unsetenv("ASCIINEMA_RECORDING")
	return *asciicast, nil
}

// 实现支持回调的录制方法
func (r *AsciicastRecorder) RecordWithCallback(command, title string, maxWait float64, assumeYes bool, env map[string]string, callback FrameCallback) (Asciicast, error) {
	rows, cols, _ := r.Terminal.Size()
	if rows > warnRows || cols > warnCols {
		if !assumeYes {
			doneChan := r.checkTerminalSize()
			util.Warningf("Current terminal size is %vx%v.", cols, rows)
			util.Warningf("It may be too big to be properly replayed on smaller screens.")
			util.Warningf("You can now resize it. Press <Enter> to start recording.")
			util.ReadLine()
			doneChan <- true
		}
	}
	os.Setenv("ASCIINEMA_RECORDING", "true")
	util.Printf("Asciicast recording with stream writing started.")
	util.Printf(`Hit Ctrl-D or type "exit" to finish.`)

	// 创建一个自定义的Stream，支持回调
	stdout := NewStreamWithCallback(maxWait, callback)

	err := r.Terminal.Record(command, stdout)
	if err != nil {
		return Asciicast{}, err
	}

	stdout.Close()

	util.Printf("Asciicast recording finished.")

	rows, cols, _ = r.Terminal.Size()

	asciicast := NewAsciicast(
		cols,
		rows,
		stdout.Duration().Seconds(),
		command,
		title,
		stdout.Frames,
		env,
	)

	os.Unsetenv("ASCIINEMA_RECORDING")
	return *asciicast, nil
}

// 获取终端大小
func (r *AsciicastRecorder) GetTerminalSize() (rows, cols int, err error) {
	return r.Terminal.Size()
}
