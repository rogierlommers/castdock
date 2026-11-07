package playback

import "errors"

var (
	ErrInvalidEpisode  = errors.New("invalid episode")
	ErrInvalidPosition = errors.New("invalid playback position")
)
