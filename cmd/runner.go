package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/x6nux/asciinema/asciicast"
	"github.com/x6nux/asciinema/util"
)

type Runner struct {
	Title           string
	MaxWait         float64
	AssumeYes       bool
	Quite           bool
	FilePath        string
	Cast            *asciicast.Asciicast
	StreamWrite     bool
	SyncInterval    int64 // 同步间隔（毫秒）
	DisableCompress bool  // 是否禁用压缩
	CompressRatio   int   // 压缩比例，值越大压缩效果越明显但可能影响回放质量
}

func New(filename ...string) (r *Runner) {
	r = &Runner{
		Title:           "asciinema_default",
		MaxWait:         1.0,
		AssumeYes:       false,
		Quite:           false,
		FilePath:        "asciinema_default.cast",
		StreamWrite:     false,
		SyncInterval:    500,   // 默认500毫秒
		DisableCompress: false, // 默认启用压缩
		CompressRatio:   8,     // 默认压缩比例为8
	}
	if len(filename) > 0 {
		r.FilePath = filename[0]
		name := filepath.Base(r.FilePath)
		r.Title = strings.Split(name, ".")[0]
	}
	initAsciinema()
	return
}

/*
Envs
*/
const (
	Version = "1.2.0"
)

var (
	cfg *util.Config
	env map[string]string
)

func showCursorBack() {
	fmt.Fprintf(os.Stdout, "\x1b[?25h")
}

func initAsciinema() {
	env = map[string]string{}
	for _, keyval := range os.Environ() {
		pair := strings.SplitN(keyval, "=", 2)
		env[pair[0]] = pair[1]
	}

	if runtime.GOOS != "windows" && !util.IsUtf8Locale(env) {
		fmt.Println("asciinema needs a UTF-8 native locale to run. Check the output of `locale` command.")
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		showCursorBack()
		os.Exit(1)
	}()
	defer showCursorBack()

	var err error
	cfg, err = util.GetConfig(env)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
