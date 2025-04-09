package asciicast

import (
	"time"
)

type PlayerTerminal interface {
	Write([]byte) (int, error)
}

type Player interface {
	Play(*Asciicast, float64) error
}

type AsciicastPlayer struct {
	Terminal PlayerTerminal
}

func NewPlayer(terminal PlayerTerminal) Player {
	return &AsciicastPlayer{Terminal: terminal}
}

func (r *AsciicastPlayer) Play(asciicast *Asciicast, maxWait float64) error {
	lenFrames := len(asciicast.Stdout)
	for i, frame := range asciicast.Stdout {
		delay := frame.Time
		if i < lenFrames-1 {
			delay = asciicast.Stdout[i+1].Time - delay
		} else {
			delay = asciicast.Stdout[i].Time - delay
		}
		if maxWait > 0 && delay > maxWait {
			delay = maxWait
		}
		r.Terminal.Write(frame.EventData)
		time.Sleep(time.Duration(float64(time.Second) * delay))
	}

	return nil
}
