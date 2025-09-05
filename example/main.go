package main

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/hekmon/go-vobsub"
)

const (
	// you must pass the .sub file but the .idx file must be present too !
	subFile = "/path/to/you/subtitle.sub"
)

func main() {
	fullSizeImages := true
	subs, skipped, err := vobsub.Decode(subFile, fullSizeImages)
	if err != nil {
		panic(err)
	}
	if skipped > 0 {
		fmt.Printf("Skipped %d bad subtitles\n", skipped)
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
