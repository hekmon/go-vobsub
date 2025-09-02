package vobsub

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"
)

func Decode(idxFile string) (subtitles []Subtitle, err error) {
	// Verify and prepare files path
	extension := filepath.Ext(idxFile)
	if extension != ".idx" {
		err = fmt.Errorf("expected .idx file extension: got %q", extension)
		return
	}
	subFile := idxFile[:len(idxFile)-len(extension)] + ".sub"
	// Parse Idx file to get subtitle metadata
	metadata, err := readIdxFile(idxFile)
	if err != nil {
		err = fmt.Errorf("failed to read Idx file: %w", err)
		return
	}
	fmt.Printf("Idx metadata: %+v\n", metadata)
	// Parse Sub
	privateStream1Packets, err := readSubFile(subFile)
	if err != nil {
		err = fmt.Errorf("failed to read .sub file: %w", err)
		return
	}
	// Concat splitted packets
	subtitlesPackets := make([]PESPacket, 0, len(privateStream1Packets))
	for _, pkt := range privateStream1Packets {
		if pkt.Header.Extension.Data.ComputePTS() != 0 {
			// New subtitle
			subtitlesPackets = append(subtitlesPackets, pkt)
		} else {
			// Subtitle has been split in multiples packets, concat to current sub
			currentSub := subtitlesPackets[len(subtitlesPackets)-1]
			currentSub.Payload = append(currentSub.Payload, pkt.Payload...)
			subtitlesPackets[len(subtitlesPackets)-1] = currentSub
		}
	}
	// Decode raw subtitles to final subtitles
	subtitles = make([]Subtitle, 0, len(subtitlesPackets))
	var (
		rawSub     SubtitleRAW
		pts        time.Duration
		startDelay time.Duration
		stopDelay  time.Duration
		subImg     image.Image
	)
	for index, subPkt := range subtitlesPackets {
		// fmt.Printf("Subtitle #%d -> (Stream ID #%d) Presentation TimeStamp: %s Payload: %d\n",
		// 	index+1, subPkt.Header.SubStreamID.SubtitleID(), subPkt.Header.Extension.Data.ComputePTS(), len(subPkt.Payload),
		// )
		// Extract raw subtitle from packet
		if rawSub, err = subPkt.ExtractSubtitle(); err != nil {
			err = fmt.Errorf("failed to parse subtitle %d: %w", index, err)
			return
		}
		// for _, ctrlSequence := range subtitle.ControlSequences {
		// 	fmt.Printf("\t%s\n", ctrlSequence)
		// }
		// Generate the image
		if subImg, startDelay, stopDelay, err = rawSub.Decode(metadata); err != nil {
			err = fmt.Errorf("failed to decode subtitle: %w", err)
			return
		}
		// Create the final subtitle
		pts = subPkt.Header.Extension.Data.ComputePTS()
		subtitles = append(subtitles, Subtitle{
			Start: pts + startDelay,
			Stop:  pts + stopDelay,
			Image: subImg,
		})
	}
	// Security check: some (rare) subtitles do not have stopDate, resulting in a stopDelay at 0 and so a 0 duration
	// To fix this we will be using the next subtitle start date and remove 100 milliseconds to compute a stop value
	// different from the start value allowing the subtitle to be shown
	for index, sub := range subtitles {
		if sub.Start == sub.Stop {
			// fmt.Println("Found a buggy sub !")
			if index+1 < len(subtitles) {
				potentialStop := subtitles[index+1].Start - 100*time.Millisecond
				if potentialStop > sub.Start {
					sub.Stop = potentialStop
					subtitles[index] = sub
					// fmt.Printf("Sub fixed ! Start: %s Stop: %s\n", subtitles[index].Start, subtitles[index].Stop)
				} // else nothing we can do (it might work with less than 100ms but it won't be readable either way[too fast])
			} // else nothing we can do here
		}
	}
	return
}

func readIdxFile(Idxfile string) (metadata IdxMetadata, err error) {
	// Open the binary sub file
	fd, err := os.Open(Idxfile)
	if err != nil {
		err = fmt.Errorf("failed to open file: %w", err)
		return
	}
	defer fd.Close()
	// Parse its metadata
	if metadata, err = parseIdx(fd); err != nil {
		err = fmt.Errorf("failed to parse Idx metadata file: %w", err)
		return
	}
	return
}

func readSubFile(subFile string) (privateStream1Packets []PESPacket, err error) {
	// Open the binary sub file
	fd, err := os.Open(subFile)
	if err != nil {
		err = fmt.Errorf("failed to open file: %w", err)
		return
	}
	defer fd.Close()
	// Parse its packets
	var (
		nextAt int64
		packet PESPacket
	)
	for nextAt != stopFlagValue {
		if packet, nextAt, err = parsePacket(fd, nextAt); err != nil {
			err = fmt.Errorf("failed to parse packet: %w", err)
			return
		}
		if packet.Header.MPH.StreamID() == StreamIDPrivateStream1 {
			privateStream1Packets = append(privateStream1Packets, packet)
		}
	}
	return
}
