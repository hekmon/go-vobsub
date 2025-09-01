package vobsub

import (
	"fmt"
	"os"
)

func ReadVobSub(subFile string) (err error) {
	// Parse IDX
	//// TODO
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
	// Handle retained subtitles
	for index, subPkt := range subtitlesPackets {
		fmt.Printf("Subtitle #%d -> (Stream ID #%d) Start: %s Payload: %d\n",
			index+1, subPkt.Header.SubStreamID.SubtitleID(), subPkt.Header.Extension.Data.ComputePTS(), len(subPkt.Payload),
		)
		_, err = subPkt.ExtractSubtitle()
		if err != nil {
			err = fmt.Errorf("failed to parse subtitle %d: %w", index, err)
			return
		}
	}
	return
}

func readSubFile(subFile string) (privateStream1Packets []PESPacket, err error) {
	// Open the binary sub file
	fd, err := os.Open(subFile)
	if err != nil {
		err = fmt.Errorf("failed to open subtitle file: %w", err)
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
