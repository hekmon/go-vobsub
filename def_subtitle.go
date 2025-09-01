package vobsub

import (
	"errors"
	"fmt"
	"image/color"
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
	subtitleCTRLSeqCmdAlphaChannelRatio   = float64(1) / float64(16) // Alphas levels are encoded on 4 bits : 0 (transparent) to 15 (opaque)
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

func (sr SubtitleRAW) Convert(palette color.Palette) (err error) {
	// Consolidate rendering metadata
	var (
		startDelay, stopDelay time.Duration
		paletteColors         *ControlSequencePalette
		alphaChannels         *ControlSequenceAlphaChannels
		coordinates           *ControlSequenceCoordinates
		RLEOffsets            *ControlSequenceRLEOffsets
	)
	for _, cs := range sr.ControlSequences {
		if cs.StartDate {
			startDelay = cs.Date.GetDelay()
		} else if cs.StopDate {
			stopDelay = cs.Date.GetDelay()
		}
		if cs.PaletteColors != nil {
			paletteColors = cs.PaletteColors
		}
		if cs.AlphaChannels != nil {
			alphaChannels = cs.AlphaChannels
		}
		if cs.Coordinates != nil {
			coordinates = cs.Coordinates
		}
		if cs.RLEOffsets != nil {
			RLEOffsets = cs.RLEOffsets
		}
	}
	if paletteColors == nil {
		err = fmt.Errorf("missing palette colors ids in subtitle")
		return
	}
	if alphaChannels == nil {
		err = fmt.Errorf("missing alpha channels ids in subtitle")
		return
	}
	if coordinates == nil {
		err = fmt.Errorf("missing coordinates in subtitle")
		return
	}
	if RLEOffsets == nil {
		err = fmt.Errorf("missing RLE offsets in subtitle")
		return
	}
	// Ready to decode
	fmt.Printf("Start delay: %s, Stop delay: %s\n", startDelay, stopDelay)
	startOffset, _ := RLEOffsets.Get()
	nibbles := nibbleIterator{data: sr.Data, index: startOffset}
	nbZeroesEncountered := 0
	nbPixels := 0
	nbLines := 0
	for nibble, _, ok := nibbles.Next(); ok; nibble, _, ok = nibbles.Next() {
		switch nbZeroesEncountered {
		case 0, 1:
			switch nibble {
			case 0xf, 0xe, 0xd, 0xc, 0xb, 0xa, 0x9, 0x8, 0x7, 0x6, 0x5, 0x4:
				fmt.Printf("Color: %d | Repeat: %d\n", nibble&0b00000011, nibble>>2)
				nbZeroesEncountered = 0
				nbPixels += int(nibble >> 2)
			case 0x3, 0x2, 0x1:
				value := nibble << 4
				if nibble, _, ok = nibbles.Next(); !ok {
					panic("argA")
				}
				value |= nibble
				fmt.Printf("Color: %d | Repeat: %d\n", value&0b00000011, value>>2)
				nbZeroesEncountered = 0
				nbPixels += int(value >> 2)
			case 0x0:
				nbZeroesEncountered++
			default:
				panic("argB")
			}
		case 2:
			var high bool
			// line carriage, read the last nimble as start a new line
			if nibble, high, ok = nibbles.Next(); !ok {
				panic("argC")
			}
			if nibble != 0 {
				// after 2 0-nimble, only possibilities in the alphabet should be a third 0
				panic(fmt.Sprintf("argD: 0b%04b\n", nibble))
			}
			if high {
				// decoder must be byte aligned, discard the last nimble before commencing the new line
				if _, _, ok = nibbles.Next(); !ok {
					panic("argE")
				}
				fmt.Println("aligning")
			}
			// new line
			nbZeroesEncountered = 0
			fmt.Printf("NEW LINE (%d pixels)\n", nbPixels)
			nbPixels = 0
			nbLines++
		default:
			panic("argF")
		}
	}
	_, height := coordinates.Get().Size()
	fmt.Printf("%d lines (height: %d)\n", nbLines, height)
	return
}

type ControlSequence struct {
	Date            ControlSequenceDate
	ForceDisplaying bool
	StartDate       bool
	StopDate        bool
	PaletteColors   *ControlSequencePalette
	AlphaChannels   *ControlSequenceAlphaChannels
	Coordinates     *ControlSequenceCoordinates
	RLEOffsets      *ControlSequenceRLEOffsets
}

type ControlSequenceDate [subtitleCTRLSeqDateLen]byte

// GetDelay convert the control sequence date to the actual delay it represents
func (csd ControlSequenceDate) GetDelay() time.Duration {
	return time.Duration(int(csd[0])<<8|int(csd[1])) * (time.Second / 100)
}

type ControlSequencePalette [subtitleCTRLSeqCmdPaletteArgsLen]byte

// GetPaletteIDs returns the 4 palette IDs colors that are used by the subtitle
func (csp ControlSequencePalette) GetIDs() (color1, color2, color3, color4 uint8) {
	color1 = uint8(csp[0] & 0b11110000 >> 4)
	color2 = uint8(csp[0] & 0b00001111)
	color3 = uint8(csp[1] & 0b11110000 >> 4)
	color4 = uint8(csp[1] & 0b00001111)
	return
}

type ControlSequenceAlphaChannels [subtitleCTRLSeqCmdAlphaChannelArgsLen]byte

// GetAlphaChannelRatios return the ratios of the alpha channels used by the 4 colors of the subtitle.
// 0 means full transparent, 1 means 100% opaque (actually 100% of the maximum opacity defined in the idx file, often 100% itself)
func (csac ControlSequenceAlphaChannels) GetRatios() (alpha1, alpha2, alpha3, alpha4 float64) {
	alpha1 = float64(int(csac[0]&0b11110000>>4)) * subtitleCTRLSeqCmdAlphaChannelRatio
	alpha2 = float64(int(csac[0]&0b00001111)) * subtitleCTRLSeqCmdAlphaChannelRatio
	alpha3 = float64(int(csac[1]&0b11110000>>4)) * subtitleCTRLSeqCmdAlphaChannelRatio
	alpha4 = float64(int(csac[1]&0b00001111)) * subtitleCTRLSeqCmdAlphaChannelRatio
	return
}

type ControlSequenceCoordinates [subtitleCTRLSeqCmdCoordinatesArgsLen]byte

// GetCoordinates returns the coordinates of the subtitle canvea on the screen : x1, x2, y1, y2
func (csc ControlSequenceCoordinates) Get() (coord SubtitleCoordinates) {
	coord.Point1.X = int(csc[0])<<4 | int(csc[1]&0b11110000)>>4
	coord.Point2.X = int(csc[1]&0b00001111)<<8 | int(csc[2])
	coord.Point1.Y = int(csc[3])<<4 | int(csc[4]&0b11110000)>>4
	coord.Point2.Y = int(csc[4]&0b00001111)<<8 | int(csc[5])
	return
}

type ControlSequenceRLEOffsets [subtitleCTRLSeqCmdRLEOffsetsArgsLen]byte

func (csrleo ControlSequenceRLEOffsets) Get() (firstLineOffset int, secondLineOffset int) {
	firstLineOffset = int(csrleo[0])<<8 | int(csrleo[1])
	secondLineOffset = int(csrleo[2])<<8 | int(csrleo[3])
	return
}

func (cs ControlSequence) String() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Delay: %s", cs.Date.GetDelay()))
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
		c1, c2, c3, c4 := cs.PaletteColors.GetIDs()
		builder.WriteString(
			fmt.Sprintf(" | Palette: color1(%d) color2(%d) color3(%d) color4(%d)",
				c1, c2, c3, c4,
			),
		)
	}
	// AlphaChannel
	if cs.AlphaChannels != nil {
		c1, c2, c3, c4 := cs.AlphaChannels.GetRatios()
		builder.WriteString(
			fmt.Sprintf(" | AlphaChannels: color1(%f) color2(%f) color3(%f) color4(%f)",
				c1, c2, c3, c4,
			),
		)
	}
	// Coordinates
	if cs.Coordinates != nil {
		coord := cs.Coordinates.Get()
		builder.WriteString(
			fmt.Sprintf(" | Coordinates: x1(%d) x2(%d) y1(%d) y2(%d)",
				coord.Point1.X, coord.Point2.X, coord.Point1.Y, coord.Point2.Y,
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
		firstLineOffset, secondLineOffset := cs.RLEOffsets.Get()
		builder.WriteString(
			fmt.Sprintf(" | RLE Offsets: 1st(%d) 2nd(%d)", firstLineOffset, secondLineOffset),
		)
	}
	return builder.String()
}

type SubtitleCoordinates struct {
	Point1, Point2 Coordinate
}

type Coordinate struct {
	X, Y int
}

func (coord SubtitleCoordinates) Size() (width, height int) {
	return coord.Point2.X - coord.Point1.X + 1, coord.Point2.Y - coord.Point1.Y + 1
}

/*
	Extract helpers
*/

type nibbleIterator struct {
	data []byte
	// instructions for next read
	index   int
	readLow bool
}

func (ni *nibbleIterator) Next() (nibble byte, high, ok bool) {
	if ni.index >= len(ni.data) {
		return
	}
	ok = true
	if !ni.readLow {
		// First read at index
		high = true
		nibble = (ni.data[ni.index] & 0b11110000) >> 4
	} else {
		// Second read at index
		high = false
		nibble = (ni.data[ni.index] & 0b00001111)
		ni.index++
	}
	ni.readLow = !ni.readLow
	return
}

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
			cs.PaletteColors = new(ControlSequencePalette)
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
			cs.AlphaChannels = new(ControlSequenceAlphaChannels)
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
			cs.Coordinates = new(ControlSequenceCoordinates)
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
			cs.RLEOffsets = new(ControlSequenceRLEOffsets)
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
