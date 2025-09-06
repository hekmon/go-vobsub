package vobsub

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"
)

// as .sub files can contains multilples streams, the returns map contains all streams with their ID as key.
// Most of sub files only contains one stream (ID 0).
func Decode(subFile string, fullSizeImages bool) (subtitles map[int][]Subtitle, skippedBadSub []error, err error) {
	// Verify and prepare files path
	extension := filepath.Ext(subFile)
	if extension != ".sub" {
		err = fmt.Errorf("expected .sub file extension: got %q", extension)
		return
	}
	idxFile := subFile[:len(subFile)-len(extension)] + ".idx"
	// Parse Idx file to get subtitle metadata
	metadata, err := readIdxFile(idxFile)
	if err != nil {
		err = fmt.Errorf("failed to read Idx file: %w", err)
		return
	}
	// fmt.Printf("Idx metadata: %+v\n", metadata)
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
	subtitles = make(map[int][]Subtitle, 1)
	var (
		rawSub     SubtitleRAW
		pts        time.Duration
		streamSubs []Subtitle
		found      bool
		startDelay time.Duration
		stopDelay  time.Duration
		subImg     image.Image
	)
	for index, subPkt := range subtitlesPackets {
		fmt.Printf("Subtitle #%d -> (Stream ID #%d) Presentation TimeStamp: %s Payload: %d\n",
			index+1, subPkt.Header.SubStreamID.SubtitleID(), subPkt.Header.Extension.Data.ComputePTS(), len(subPkt.Payload),
		)
		// Recover the current stream subs slice
		if streamSubs, found = subtitles[subPkt.Header.SubStreamID.SubtitleID()]; !found {
			streamSubs = make([]Subtitle, 0, len(subtitlesPackets))
		}
		// Extract raw subtitle from packet
		if rawSub, err = subPkt.ExtractSubtitle(); err != nil {
			// Encountered some bad packets in the wild: discarding them
			// I compared with Subtitle Edit nothing was missing, it seems SE did skip them too
			skippedBadSub = append(skippedBadSub, fmt.Errorf("packet #%d: %w", index+1, err))
			err = nil
			continue
		}
		// for _, ctrlSequence := range subtitle.ControlSequences {
		// 	fmt.Printf("\t%s\n", ctrlSequence)
		// }
		// Generate the image
		if subImg, startDelay, stopDelay, err = rawSub.Decode(metadata, fullSizeImages); err != nil {
			err = fmt.Errorf("failed to decode subtitle: %w", err)
			return
		}
		// Create the final subtitle
		pts = subPkt.Header.Extension.Data.ComputePTS()
		streamSubs = append(streamSubs, Subtitle{
			Start: metadata.TimeOffset + pts + startDelay,
			Stop:  metadata.TimeOffset + pts + stopDelay,
			Image: subImg,
		})
		subtitles[subPkt.Header.SubStreamID.SubtitleID()] = streamSubs
	}
	// Security check: some (rare) subtitles do not have stopDate, resulting in a stopDelay at 0 and so a 0 duration
	// To fix this we will be using the next subtitle start date and remove 100 milliseconds to compute a stop value
	// different from the start value thus allowing the subtitle to be shown
	for _, streamSubs := range subtitles {
		for index, sub := range streamSubs {
			if sub.Start == sub.Stop {
				// fmt.Println("Found a buggy sub !")
				if index+1 < len(subtitles) {
					potentialStop := streamSubs[index+1].Start - 100*time.Millisecond
					if potentialStop > sub.Start {
						sub.Stop = potentialStop
						streamSubs[index] = sub
						// fmt.Printf("Sub fixed ! Start: %s Stop: %s\n", subtitles[index].Start, subtitles[index].Stop)
					} // else nothing we can do (it might work with less than 100ms but it won't be readable either way[too fast])
				} // else nothing we can do here
			}
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
