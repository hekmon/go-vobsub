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
	// Filter private stream 1 packets
	subtitlesPackets := make([]PESPacket, 0, len(privateStream1Packets))
	for _, pkt := range privateStream1Packets {
		if pkt.Header.Extension.Data.ComputePTS() != 0 {
			subtitlesPackets = append(subtitlesPackets, pkt)
		}
	}
	// Handle retained subtitles
	for index, sub := range subtitlesPackets {
		fmt.Printf("Subtitle #%d -> (Stream ID #%d) Start: %s Payload: %d\n",
			index+1, sub.Header.SubStreamID.SubtitleID(), sub.Header.Extension.Data.ComputePTS(), len(sub.Payload),
		)
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
	var (
		nextAt int64
		packet PESPacket
	)
	for nextAt != stopFlagValue {
		if packet, nextAt, err = parsePacket(fd, nextAt); err != nil {
			err = fmt.Errorf("failed to parse packet: %w", err)
			return
		}
		if packet.Header.StartCodeHeader.StreamID() == StreamIDPrivateStream1 {
			privateStream1Packets = append(privateStream1Packets, packet)
		}
	}
	return
}
