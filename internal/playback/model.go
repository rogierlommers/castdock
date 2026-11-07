package playback

import "time"

type State struct {
	EpisodeID       int64
	Position        float64
	Duration        float64
	Completed       bool
	Playing         bool
	ClientUpdatedAt time.Time
}

func (s State) Validate() error {
	if s.EpisodeID <= 0 {
		return ErrInvalidEpisode
	}
	if s.Position < 0 || s.Duration < 0 {
		return ErrInvalidPosition
	}
	if s.Duration > 0 && s.Position > s.Duration+1 {
		return ErrInvalidPosition
	}
	return nil
}
