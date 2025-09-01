package vobsub

import (
	"errors"
	"fmt"
	"time"
)

const (
	subtitleCTRLSeqCmdForceDisplaying     = 0x00
	subtitleCTRLSeqCmdStartDate           = 0x01
	subtitleCTRLSeqCmdStopDate            = 0x02
	subtitleCTRLSeqCmdPalette             = 0x03
	subtitleCTRLSeqCmdPaletteArgsLen      = 2
	subtitleCTRLSeqCmdAlphaChannel        = 0x04
	subtitleCTRLSeqCmdAlphaChannelArgsLen = 2
	subtitleCTRLSeqCmdCoordinates         = 0x05
	subtitleCTRLSeqCmdCoordinatesArgsLen  = 6
	subtitleCTRLSeqCmdRLEOffsets          = 0x06
	subtitleCTRLSeqCmdRLEOffsetsArgsLen   = 4
	subtitleCTRLSeqCmdEnd                 = 0xff
)

type Subtitle struct {
	data []byte
}

func parseSubtitle(packet PESPacket) (subtitle Subtitle, err error) {
	// Read the size first
	size := int(packet.Payload[0])<<8 | int(packet.Payload[1])
	// fmt.Printf("Packet len: 0b%08b 0b%08b -> %d\n", packet.Payload[0], packet.Payload[1], size)
	if len(packet.Payload) != size {
		err = fmt.Errorf("the read packet size (%d) does not match the received packet length (%d)", size, len(packet.Payload))
		return
	}
	// Read the data packet size in order to split the data and the control sequences
	dataSize := int(packet.Payload[2])<<8 | int(packet.Payload[3])
	// fmt.Printf("Data Packet len: 0b%08b 0b%08b -> %d\n", packet.Payload[2], packet.Payload[3], dataSize)
	if dataSize > len(packet.Payload)-2 {
		err = fmt.Errorf("the read data packet size (%d) is greater than the total packet size (%d)", size, len(packet.Payload))
		return
	}
	// Split
	subtitle.data = packet.Payload[4:dataSize]
	ctrlseqs := packet.Payload[dataSize:]
	fmt.Printf("Data len is %d and ctrl seq len is %d\n", len(subtitle.data), len(ctrlseqs))
	fmt.Printf("Control Sequence: %08b\n", ctrlseqs)
	// Parse control sequences
	err = parseCTRLSeqs(ctrlseqs, dataSize)
	if err != nil {
		err = fmt.Errorf("failed to parse control sequences: %w", err)
		return
	}
	// Decode image
	//// TODO
	return
}

func parseCTRLSeqs(sequences []byte, baseOffset int) (err error) {
	index := 0
	nbSeqs := 0
	nextOffset := 0
	lastIndex := 0
	for {
		nbSeqs++
		if nextOffset, lastIndex, err = parseCTRLSeq(sequences, index); err != nil {
			err = fmt.Errorf("failed to parse control seq #%d: %w", nbSeqs, err)
			return
		}
		if (nextOffset - baseOffset) == index {
			// next offset is us, meaning we are the last control seq
			break
		}
		index = nextOffset - baseOffset
	}
	fmt.Printf("read %d sequences, last index is %d on %d\n", nbSeqs, lastIndex, len(sequences))
	for i := lastIndex; i < len(sequences); i++ {
		if sequences[i] != 0xff {
			err = errors.New("control sequences post commands bytes are not padding")
			return
		}
	}
	return
}

func parseCTRLSeq(sequences []byte, index int) (nextOffset, lastIndex int, err error) {
	fmt.Println("Sequence CTRL")
	if index+4 > len(sequences) {
		err = fmt.Errorf("can not parse sequence: current index is %d and sequences length is %d: need at least 4 bytes to read date and next offset",
			index, len(sequences),
		)
		return
	}
	// Extract date
	date := int(sequences[index+0])<<8 | int(sequences[index+1])
	index += 2
	delay := time.Duration(date) * (time.Second / 100)
	fmt.Println(" Delay is", delay)
	// Extract next sequence offset
	nextOffset = int(sequences[index+0])<<8 | int(sequences[index+1])
	index += 2
	fmt.Println(" next offset is", nextOffset)
	// Read commands
commands:
	for {
		if index >= len(sequences) {
			err = fmt.Errorf("can not read sequence command: index is %d and sequences length is %d: need at least one byte to read the command",
				index, len(sequences),
			)
			return
		}
		cmd := sequences[index]
		index++
		switch cmd {
		case subtitleCTRLSeqCmdForceDisplaying:
			fmt.Println("  Force displaying")
		case subtitleCTRLSeqCmdStartDate:
			fmt.Println("  StartDateCMD")
		case subtitleCTRLSeqCmdStopDate:
			fmt.Println("  StopDateCMD")
		case subtitleCTRLSeqCmdPalette:
			fmt.Println("  PaletteCMD")
			if index+subtitleCTRLSeqCmdPaletteArgsLen > len(sequences) {
				err = fmt.Errorf("can not read palette command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdPaletteArgsLen,
				)
				return
			}
			index += subtitleCTRLSeqCmdPaletteArgsLen
		case subtitleCTRLSeqCmdAlphaChannel:
			fmt.Println("  AlphaChannelCMD")
			if index+subtitleCTRLSeqCmdAlphaChannelArgsLen > len(sequences) {
				err = fmt.Errorf("can not read alpha channel command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdAlphaChannelArgsLen,
				)
				return
			}
			index += subtitleCTRLSeqCmdAlphaChannelArgsLen
		case subtitleCTRLSeqCmdCoordinates:
			fmt.Println("  CmdCoordinatesCMD")
			if index+subtitleCTRLSeqCmdCoordinatesArgsLen > len(sequences) {
				err = fmt.Errorf("can not read coordinates command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdCoordinatesArgsLen,
				)
				return
			}
			index += subtitleCTRLSeqCmdCoordinatesArgsLen
		case subtitleCTRLSeqCmdRLEOffsets:
			fmt.Println("  RLEOffsetsCMD")
			if index+subtitleCTRLSeqCmdRLEOffsetsArgsLen > len(sequences) {
				err = fmt.Errorf("can not read RLE offsets command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdRLEOffsetsArgsLen,
				)
				return
			}
			index += subtitleCTRLSeqCmdRLEOffsetsArgsLen
		case subtitleCTRLSeqCmdEnd:
			fmt.Println("  EndCMD")
			break commands
		default:
			err = fmt.Errorf("unknown command: 0x%02x", cmd)
		}
	}
	lastIndex = index
	return
}
