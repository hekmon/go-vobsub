package main

import (
	"fmt"
	"os"
)

func main() {
	if err := ReadVobSub(file); err != nil {
		panic(err)
	}
}

func ReadVobSub(subFile string) (err error) {
	// Parse IDX
	//// TODO
	// Open the binary sub file
	fd, err := os.Open(subFile)
	if err != nil {
		err = fmt.Errorf("failed to open subtitle file: %w", err)
		return
	}
	defer fd.Close()
	// Parse each packet
	if err = parsePESPacket(fd, 0); err != nil {
		return
	}
	return
}
