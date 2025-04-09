package commands

import (
	"github.com/x6nux/asciinema/asciicast"
	"github.com/x6nux/asciinema/terminal"
)

type PlayCommand struct {
	Player terminal.Player
}

func NewPlayCommand() *PlayCommand {
	return &PlayCommand{
		Player: terminal.NewPlayer(),
	}
}

func (c *PlayCommand) Execute(cast *asciicast.Asciicast, maxWait float64) error {
	return c.Player.Play(cast, maxWait)
}
