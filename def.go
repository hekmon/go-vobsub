package vobsub

import (
	"image"
	"time"
)

type Coordinate struct {
	X, Y int
}

func (c Coordinate) IsZero() bool {
	return c.X == 0 && c.Y == 0
}

type Subtitle struct {
	Start time.Duration
	Stop  time.Duration
	Image image.Image
}
