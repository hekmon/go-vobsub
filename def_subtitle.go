package vobsub

import (
	"errors"
	"fmt"
	"time"
)

const (
	subtitleCTRLSeqDateLen                = 2
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

type SubtitleRAW struct {
	data             []byte
	ControlSequences []ControlSequence
}

type ControlSequence struct {
	Date            [subtitleCTRLSeqDateLen]byte
	ForceDisplaying bool
	StartDate       bool
	StopDate        bool
	Palette         *[subtitleCTRLSeqCmdPaletteArgsLen]byte
	AlphaChannel    *[subtitleCTRLSeqCmdAlphaChannelArgsLen]byte
	Coordinates     *[subtitleCTRLSeqCmdCoordinatesArgsLen]byte
	RLEOffsets      *[subtitleCTRLSeqCmdRLEOffsetsArgsLen]byte
}

// GetDelay convert the control sequence date to the actual delay it represents
func (cs *ControlSequence) GetDelay() time.Duration {
	return time.Duration(int(cs.Date[0])<<8|int(cs.Date[1])) * (time.Second / 100)
}

func ExtractRAWSubtitle(packet PESPacket) (subtitle SubtitleRAW, err error) {
	// Check if the packet is a subtitle packet
	if packet.Header.MPH.StreamID() != StreamIDPrivateStream1 {
		err = fmt.Errorf("the packet stream ID (%s) does not match the expected private stream 1", packet.Header.MPH.StreamID())
		return
	}
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
	// Handle subtitle data and control sequences
	subtitle.data = packet.Payload[4:dataSize]
	if subtitle.ControlSequences, err = parseCTRLSeqs(packet.Payload[dataSize:], dataSize); err != nil {
		err = fmt.Errorf("failed to parse control sequences: %w", err)
		return
	}
	return
}

func parseCTRLSeqs(sequences []byte, baseOffset int) (ctrlSeqs []ControlSequence, err error) {
	ctrlSeqs = make([]ControlSequence, 0, 2) // most of the date a subtitle will have 2 ctrl sequences: the first with coordinates, palette, etc... and the second with the stop date
	nbSeqs := 0
	index := 0
	nextOffset := 0
	lastIndex := 0
	var ctrlSeq ControlSequence
	for {
		nbSeqs++
		if ctrlSeq, nextOffset, lastIndex, err = parseCTRLSeq(sequences, index); err != nil {
			err = fmt.Errorf("failed to parse control seq #%d: %w", nbSeqs, err)
			return
		}
		ctrlSeqs = append(ctrlSeqs, ctrlSeq)
		if (nextOffset - baseOffset) == index {
			// next offset is us, meaning we are the last control seq
			break
		}
		index = nextOffset - baseOffset
	}
	for i := lastIndex; i < len(sequences); i++ {
		if sequences[i] != 0xff {
			err = errors.New("control sequences post commands bytes are not padding")
			return
		}
	}
	return
}

func parseCTRLSeq(sequences []byte, index int) (cs ControlSequence, nextOffset, lastIndex int, err error) {
	if index+4 > len(sequences) {
		err = fmt.Errorf("can not parse sequence: current index is %d and sequences length is %d: need at least 4 bytes to read date and next offset",
			index, len(sequences),
		)
		return
	}
	// Extract date
	cs.Date = [subtitleCTRLSeqDateLen]byte{
		sequences[index+0],
		sequences[index+1],
	}
	index += subtitleCTRLSeqDateLen
	// Extract next sequence offset
	nextOffset = int(sequences[index+0])<<8 | int(sequences[index+1])
	index += 2
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
			cs.ForceDisplaying = true
		case subtitleCTRLSeqCmdStartDate:
			cs.StartDate = true
		case subtitleCTRLSeqCmdStopDate:
			cs.StopDate = true
		case subtitleCTRLSeqCmdPalette:
			if index+subtitleCTRLSeqCmdPaletteArgsLen > len(sequences) {
				err = fmt.Errorf("can not read palette command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdPaletteArgsLen,
				)
				return
			}
			cs.Palette = new([subtitleCTRLSeqCmdPaletteArgsLen]byte)
			for i := range subtitleCTRLSeqCmdPaletteArgsLen {
				cs.Palette[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdPaletteArgsLen
		case subtitleCTRLSeqCmdAlphaChannel:
			if index+subtitleCTRLSeqCmdAlphaChannelArgsLen > len(sequences) {
				err = fmt.Errorf("can not read alpha channel command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdAlphaChannelArgsLen,
				)
				return
			}
			cs.AlphaChannel = new([subtitleCTRLSeqCmdAlphaChannelArgsLen]byte)
			for i := range subtitleCTRLSeqCmdAlphaChannelArgsLen {
				cs.AlphaChannel[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdAlphaChannelArgsLen
		case subtitleCTRLSeqCmdCoordinates:
			if index+subtitleCTRLSeqCmdCoordinatesArgsLen > len(sequences) {
				err = fmt.Errorf("can not read coordinates command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdCoordinatesArgsLen,
				)
				return
			}
			cs.Coordinates = new([subtitleCTRLSeqCmdCoordinatesArgsLen]byte)
			for i := range subtitleCTRLSeqCmdCoordinatesArgsLen {
				cs.Coordinates[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdCoordinatesArgsLen
		case subtitleCTRLSeqCmdRLEOffsets:
			if index+subtitleCTRLSeqCmdRLEOffsetsArgsLen > len(sequences) {
				err = fmt.Errorf("can not read RLE offsets command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdRLEOffsetsArgsLen,
				)
				return
			}
			cs.RLEOffsets = new([subtitleCTRLSeqCmdRLEOffsetsArgsLen]byte)
			for i := range subtitleCTRLSeqCmdRLEOffsetsArgsLen {
				cs.RLEOffsets[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdRLEOffsetsArgsLen
		case subtitleCTRLSeqCmdEnd:
			break commands
		default:
			err = fmt.Errorf("unknown command: 0x%02x", cmd)
		}
	}
	lastIndex = index
	return
}
