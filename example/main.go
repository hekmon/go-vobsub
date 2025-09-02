package main

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/hekmon/go-vobsub"
)

const (
	// you must pass the .idx file but the .sub file must be present too !
	idxFile = "/path/to/you/subtitle.idx"
)

func main() {
	subs, _, err := vobsub.Decode(idxFile)
	if err != nil {
		panic(err)
	}
	for index, sub := range subs {
		filename := fmt.Sprintf("sub_%03d.png", index+1)
		fmt.Printf("Subtitle #%d: %s --> %s\n", index+1, sub.Start, sub.Stop)
		if err = writeSub(filename, sub.Image); err != nil {
			panic(err)
		}
	}
}

func writeSub(filename string, img image.Image) (err error) {
	file, err := os.Create(filename)
	if err != nil {
		return
	}
	defer file.Close()
	return png.Encode(file, img)
}
