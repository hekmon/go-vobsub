package vobsub

import (
	"fmt"
	"os"
	"path/filepath"
)

func ReadVobSub(idxFile string) (err error) {
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
	// Extract raw subtitles from packets
	rawSubtitles := make([]SubtitleRAW, 0, len(subtitlesPackets))
	for index, subPkt := range subtitlesPackets {
		fmt.Printf("Subtitle #%d -> (Stream ID #%d) Presentation TimeStamp: %s Payload: %d\n",
			index+1, subPkt.Header.SubStreamID.SubtitleID(), subPkt.Header.Extension.Data.ComputePTS(), len(subPkt.Payload),
		)
		var subtitle SubtitleRAW
		if subtitle, err = subPkt.ExtractSubtitle(); err != nil {
			err = fmt.Errorf("failed to parse subtitle %d: %w", index, err)
			return
		}
		rawSubtitles = append(rawSubtitles, subtitle)
		// for _, ctrlSequence := range subtitle.ControlSequences {
		// 	fmt.Printf("\t%s\n", ctrlSequence)
		// }
	}
	// Convert raw subtitles to final image subtitles
	for _, rawSubtitle := range rawSubtitles {
		if err = rawSubtitle.Convert(metadata); err != nil {
			err = fmt.Errorf("failed to decode subtitle: %w", err)
			return
		}
		fmt.Println()
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
