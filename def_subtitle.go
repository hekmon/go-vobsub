package vobsub

import (
	"errors"
	"fmt"
	"strings"
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
	Data             []byte
	ControlSequences []ControlSequence
}

type ControlSequence struct {
	Date            [subtitleCTRLSeqDateLen]byte
	ForceDisplaying bool
	StartDate       bool
	StopDate        bool
	PaletteColors   *[subtitleCTRLSeqCmdPaletteArgsLen]byte
	AlphaChannels   *[subtitleCTRLSeqCmdAlphaChannelArgsLen]byte
	Coordinates     *[subtitleCTRLSeqCmdCoordinatesArgsLen]byte
	RLEOffsets      *[subtitleCTRLSeqCmdRLEOffsetsArgsLen]byte
}

// GetDelay convert the control sequence date to the actual delay it represents
func (cs ControlSequence) GetDelay() time.Duration {
	return time.Duration(int(cs.Date[0])<<8|int(cs.Date[1])) * (time.Second / 100)
}

// GetPalette returns the palette IDs that are used by the 4 subtitle colors
func (cs ControlSequence) GetPalette() (color1, color2, color3, color4 PaletteColorID) {
	if cs.PaletteColors == nil {
		return
	}
	color1 = PaletteColorID(cs.PaletteColors[0] & 0b11110000 >> 4)
	color2 = PaletteColorID(cs.PaletteColors[0] & 0b00001111)
	color3 = PaletteColorID(cs.PaletteColors[1] & 0b11110000 >> 4)
	color4 = PaletteColorID(cs.PaletteColors[1] & 0b00001111)
	return
}

func (cs ControlSequence) GetAlphaChannels() (color1, color2, color3, color4 uint8) {
	color1 = uint8(cs.AlphaChannels[0] & 0b11110000 >> 4)
	color2 = uint8(cs.AlphaChannels[0] & 0b00001111)
	color3 = uint8(cs.AlphaChannels[1] & 0b11110000 >> 4)
	color4 = uint8(cs.AlphaChannels[1] & 0b00001111)
	return
}

// GetCoordinates returns the coordinates of the subtitle on the screen : x1, x2, y1, y2
func (cs ControlSequence) GetCoordinates() (coord SubtitleCoordinate) {
	if cs.Coordinates == nil {
		return
	}
	coord.X1 = int(cs.Coordinates[0])<<4 | int(cs.Coordinates[1]&0b11110000)>>4
	coord.X2 = int(cs.Coordinates[1]&0b00001111)<<8 | int(cs.Coordinates[2])
	coord.Y1 = int(cs.Coordinates[3])<<4 | int(cs.Coordinates[4]&0b11110000)>>4
	coord.Y2 = int(cs.Coordinates[4]&0b00001111)<<8 | int(cs.Coordinates[5])
	return
}

func (cs ControlSequence) GetRLEOffsets() (firstLineOffset int, secondLineOffset int) {
	if cs.RLEOffsets == nil {
		return
	}
	firstLineOffset = int(cs.RLEOffsets[0])<<8 | int(cs.RLEOffsets[1])
	secondLineOffset = int(cs.RLEOffsets[2])<<8 | int(cs.RLEOffsets[3])
	return
}

func (cs ControlSequence) String() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Delay: %s", cs.GetDelay()))
	// Force Displaying
	if cs.ForceDisplaying {
		builder.WriteString(" | Force Displaying")
	}
	// Start Date
	if cs.StartDate {
		builder.WriteString(" | StartDate")
	}
	// Stop Date
	if cs.StopDate {
		builder.WriteString(" | StopDate")
	}
	// Palette
	if cs.PaletteColors != nil {
		c1, c2, c3, c4 := cs.GetPalette()
		builder.WriteString(
			fmt.Sprintf(" | Palette: color1(%d) color2(%d) color3(%d) color4(%d)",
				c1, c2, c3, c4,
			),
		)
	}
	// AlphaChannel
	if cs.AlphaChannels != nil {
		c1, c2, c3, c4 := cs.GetAlphaChannels()
		builder.WriteString(
			fmt.Sprintf(" | AlphaChannels: color1(%d) color2(%d) color3(%d) color4(%d)",
				c1, c2, c3, c4,
			),
		)
	}
	// Coordinates
	if cs.Coordinates != nil {
		coord := cs.GetCoordinates()
		builder.WriteString(
			fmt.Sprintf(" | Coordinates: x1(%d) x2(%d) y1(%d) y2(%d)",
				coord.X1, coord.X2, coord.Y1, coord.Y2,
			),
		)
		width, length := coord.Size()
		builder.WriteString(
			fmt.Sprintf(" size(%dx%d)",
				width, length,
			),
		)
	}
	// RLE Offsets
	if cs.RLEOffsets != nil {
		firstLineOffset, secondLineOffset := cs.GetRLEOffsets()
		builder.WriteString(
			fmt.Sprintf(" | RLE Offsets: 1st(%d) 2nd(%d)", firstLineOffset, secondLineOffset),
		)
	}
	return builder.String()
}

// PaletteColorID contains the ID of a color in the palette. It is used to select the color for the subtitle text.
type PaletteColorID int

type SubtitleCoordinate struct {
	X1, X2 int
	Y1, Y2 int
}

func (coord SubtitleCoordinate) Size() (width, length int) {
	return coord.X2 - coord.X1 + 1, coord.Y2 - coord.Y1 + 1
}

/*
	Extract helpers
*/

func extractRAWSubtitle(packet PESPacket) (subtitle SubtitleRAW, err error) {
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
	subtitle.Data = packet.Payload[4:dataSize]
	if subtitle.ControlSequences, err = parseCTRLSeqs(packet.Payload[dataSize:], dataSize); err != nil {
		err = fmt.Errorf("failed to parse control sequences: %w", err)
		return
	}
	return
}

func parseCTRLSeqs(sequences []byte, baseOffset int) (ctrlSeqs []ControlSequence, err error) {
	ctrlSeqs = make([]ControlSequence, 0, 2) // most subtitles will have 2 ctrl sequences: the first with coordinates, palette, etc... and the second with the stop date
	nbSeqs := 0
	nextStart := 0
	nextOffset := 0
	read := 0
	var ctrlSeq ControlSequence
	for {
		nbSeqs++
		if ctrlSeq, nextOffset, read, err = parseCTRLSeq(sequences[nextStart:]); err != nil {
			err = fmt.Errorf("failed to parse control seq #%d: %w", nbSeqs, err)
			return
		}
		ctrlSeqs = append(ctrlSeqs, ctrlSeq)
		if (nextOffset - baseOffset) == nextStart {
			// next offset is ourself, meaning we are the last control seq
			nextStart += read
			break
		}
		nextStart = nextOffset - baseOffset
	}
	for i := nextStart; i < len(sequences); i++ {
		if sequences[i] != 0xff {
			err = errors.New("control sequences post commands bytes are not padding")
			return
		}
	}
	return
}

func parseCTRLSeq(sequences []byte) (cs ControlSequence, nextOffset, index int, err error) {
	if len(sequences) < 4 {
		err = fmt.Errorf("can not parse sequence: current index is %d and sequence length is %d: need at least 4 bytes to read date and next offset",
			index, len(sequences),
		)
		return
	}
	// Extract date
	cs.Date = [subtitleCTRLSeqDateLen]byte{
		sequences[0],
		sequences[1],
	}
	// Extract next sequence offset
	nextOffset = int(sequences[2])<<8 | int(sequences[3])
	// Read commands
	index = 4
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
			cs.PaletteColors = new([subtitleCTRLSeqCmdPaletteArgsLen]byte)
			for i := range subtitleCTRLSeqCmdPaletteArgsLen {
				cs.PaletteColors[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdPaletteArgsLen
		case subtitleCTRLSeqCmdAlphaChannel:
			if index+subtitleCTRLSeqCmdAlphaChannelArgsLen > len(sequences) {
				err = fmt.Errorf("can not read alpha channel command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdAlphaChannelArgsLen,
				)
				return
			}
			cs.AlphaChannels = new([subtitleCTRLSeqCmdAlphaChannelArgsLen]byte)
			for i := range subtitleCTRLSeqCmdAlphaChannelArgsLen {
				cs.AlphaChannels[i] = sequences[index+i]
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
			return
		default:
			err = fmt.Errorf("unknown command: 0x%02x", cmd)
			return
		}
	}
}
