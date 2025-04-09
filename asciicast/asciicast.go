package asciicast

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Env struct {
	Term  string `json:"TERM"`
	Shell string `json:"SHELL"`
}

type Duration float64

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`%.6f`, d)), nil
}

type Asciicast struct {
	Version   int      `json:"version"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Timestamp int64    `json:"timestamp"`
	Duration  Duration `json:"duration,omitempty"`
	Command   string   `json:"command,omitempty"`
	Title     string   `json:"title,omitempty"`
	Env       *Env     `json:"env"`
	Stdout    []Frame  `json:"stdout"`
}

func NewAsciicast(width, height int, duration float64, command, title string, frames []Frame, env map[string]string) *Asciicast {
	// {"SHELL":"powershell.exe","TERM":"ms-terminal"}
	env_ := &Env{Term: env["TERM"], Shell: env["SHELL"]}
	if runtime.GOOS == "windows" {
		env_.Term = "ms-terminal"
		if strings.Contains(command, "powershell") {
			env_.Shell = "powershell.exe"
		} else {
			env_.Shell = "cmd.exe"
		}
	}
	return &Asciicast{
		Version:   2,
		Width:     width,
		Height:    height,
		Duration:  Duration(duration),
		Timestamp: time.Now().Unix(),
		Command:   command,
		Title:     title,
		Env:       env_,
		Stdout:    frames,
	}
}

type Header struct {
	Version   int      `json:"version"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Timestamp int64    `json:"timestamp"`
	Duration  Duration `json:"duration,omitempty"`
	Command   string   `json:"command,omitempty"`
	Title     string   `json:"title,omitempty"`
	Env       *Env     `json:"env"`
}

// asciinema play file.json
// asciinema play https://asciinema.org/a/123.json
// asciinema play https://asciinema.org/a/123
// asciinema play ipfs://ipfs/QmbdpNCwqeZgnmAWBCQcs8u6Ts6P2ku97tfKAycE1XY88p
// asciinema play -

func extractJSONURL(htmlDoc io.Reader) (string, error) {
	data, err := io.ReadAll(htmlDoc)
	if err != nil {
		return "", err
	}

	// 使用正则表达式查找alternate链接
	re := regexp.MustCompile(`<link[^>]+rel=["']alternate["'][^>]+type=["']application/asciicast\+json["'][^>]+href=["']([^"']+)["'][^>]*>`)
	matches := re.FindSubmatch(data)

	if len(matches) < 2 {
		return "", fmt.Errorf("expected alternate <link> not found in fetched HTML document")
	}

	return string(matches[1]), nil
}

func getSource(url string) (io.ReadCloser, error) {
	var source io.ReadCloser
	var isHTML bool
	var err error

	if strings.HasPrefix(url, "ipfs:/") {
		url = fmt.Sprintf("https://ipfs.io/%v", url[6:])
	} else if strings.HasPrefix(url, "fs:/") {
		url = fmt.Sprintf("https://ipfs.io/%v", url[4:])
	}

	if url == "-" {
		source = os.Stdin
	} else if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		resp, err := http.Get(url)

		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("got status %v when requesting %v", resp.StatusCode, url)
		}

		source = resp.Body

		if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			isHTML = true
		}
	} else {
		source, err = os.Open(url)
		if err != nil {
			return nil, err
		}

		if strings.HasSuffix(url, ".html") {
			isHTML = true
		}
	}

	if isHTML {
		defer source.Close()
		url, err = extractJSONURL(source)
		if err != nil {
			return nil, err
		}

		return getSource(url)
	}

	return source, nil
}

func Load(url string) (*Asciicast, error) {
	source, err := getSource(url)
	if err != nil {
		return nil, err
	}
	defer source.Close()

	dec := json.NewDecoder(source)
	asciicast := &Asciicast{}

	if err = dec.Decode(asciicast); err != nil {
		return nil, err
	}

	return asciicast, nil
}

// 实现terminal.Cast接口
func (a *Asciicast) GetWidth() int {
	return a.Width
}

func (a *Asciicast) GetHeight() int {
	return a.Height
}

func (a *Asciicast) GetFrames() interface{} {
	return a.Stdout
}

// Frames 返回这个Asciicast的所有帧，满足terminal包中的类型断言需要
func (a *Asciicast) Frames() []interface{} {
	frames := make([]interface{}, len(a.Stdout))
	for i, f := range a.Stdout {
		frame := f // 创建局部变量避免引用问题
		frames[i] = &frame
	}
	return frames
}

// 实现terminal.Frame接口
func (f *Frame) GetTime() float64 {
	return f.Time
}

func (f *Frame) GetEventType() string {
	return f.EventType
}

func (f *Frame) GetEventData() []byte {
	return f.EventData
}

// CompressedFrameData 结构用于压缩帧数据的序列化和反序列化
type CompressedFrameData struct {
	Compressed bool      `json:"compressed"`
	Timestamps []float64 `json:"timestamps"`
	Offsets    []int     `json:"offsets"`
	Data       string    `json:"data"`
}
