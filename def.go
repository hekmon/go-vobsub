package vobsub

type Coordinate struct {
	X, Y int
}

func (c Coordinate) IsZero() bool {
	return c.X == 0 && c.Y == 0
}
