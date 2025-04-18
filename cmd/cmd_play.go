package cmd

import (
	"bufio"
	"encoding/json"
	"log"
	"os"

	"github.com/x6nux/asciinema/asciicast"
	"github.com/x6nux/asciinema/commands"
)

func (r *Runner) Play() error {
	r.loadFile()
	cmd := commands.NewPlayCommand()
	r.MaxWait = 3.0
	return cmd.Execute(r.Cast, r.MaxWait)
}

func (r *Runner) loadFile() {
	f, err := os.Open(r.FilePath)
	if err != nil {
		log.Fatalf("open file failed: %v", r.FilePath)
		os.Exit(1)
	}
	defer f.Close()
	fileScanner := bufio.NewScanner(f)
	fileScanner.Split(bufio.ScanLines)
	header := &asciicast.Header{}
	frameList := make([]asciicast.Frame, 0)
	i := 0
	for fileScanner.Scan() {
		i++
		if i == 1 {
			json.Unmarshal(fileScanner.Bytes(), header)
		} else {
			frame := asciicast.Frame{}
			if err := frame.UnmarshalJSON(fileScanner.Bytes()); err == nil {
				frameList = append(frameList, frame)
			}
		}
	}

	if r.Cast == nil {
		r.Cast = &asciicast.Asciicast{}
	}
	r.Cast.Version = header.Version
	r.Cast.Width = header.Width
	r.Cast.Height = header.Height
	r.Cast.Duration = header.Duration
	r.Cast.Timestamp = header.Timestamp
	r.Cast.Command = header.Command
	r.Cast.Title = header.Title
	r.Cast.Env = header.Env
	r.Cast.Stdout = frameList
}
