package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

const (
	// Main headers fields
	PESHeaderLen = 6
	PESStart     = 0x000001
	// Optional headers fields
	PESOptionalHeadersLen    = 3
	PESOptionalHeadersMarker = 0x2
)

func parsePESPacket(fd *os.File, startAt int64) (err error) {
	// Read header
	buffer := make([]byte, PESHeaderLen+PESOptionalHeadersLen)
	if _, err = fd.ReadAt(buffer, startAt); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	headers, err := parsePESHeaders(buffer)
	if err != nil {
		err = fmt.Errorf("failed to parse PES headers: %w", err)
		return
	}
	fmt.Printf("Parsed PES Headers: %s\n", headers)
	// Read data
	buffer = make([]byte, 0x000001000)
	if _, err = fd.ReadAt(buffer, startAt); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	fmt.Printf("Packet: %x\n", buffer)
	fmt.Printf("Packet len: buffer size: %d  packet length: %d\n", len(buffer), headers.PacketLength())
	fmt.Printf("Stuffing: %x\n", buffer[headers.PacketLength()-2:])
	return
}

func parsePESHeaders(data []byte) (headers PESHeaders, err error) {
	if len(data) != len(headers) {
		err = fmt.Errorf("data too short for PES headers: %d bytes must be %d", len(data), len(headers))
		return
	}
	copy(headers[:], data)
	if !headers.Valid() {
		err = fmt.Errorf("invalid PES headers (no PESStart code found in headers): %x", headers)
		return
	}
	return
}

// https://en.wikipedia.org/wiki/Packetized_elementary_stream#PES_packet_header
type PESHeaders [PESHeaderLen + PESOptionalHeadersLen]byte

// Valid checks if the PES start code is present at the start of the headers.
func (ph PESHeaders) Valid() bool {
	return ph[0] == byte((PESStart>>16)&0xFF) && ph[1] == byte((PESStart>>8)&0xFF) && ph[2] == byte(PESStart&0xFF)
}

func (ph PESHeaders) StreamID() StreamID {
	return StreamID(ph[3])
}

func (ph PESHeaders) PacketLength() int64 {
	return 4 + int64(binary.LittleEndian.Uint16(ph[4:6]))
}

func (ph PESHeaders) DataOffset() int64 {
	return PESHeaderLen + PESOptionalHeadersLen + ph.OptionalHeaders().RemainingHeaderLength()
}

func (ph PESHeaders) OptionalHeaders() PESOptionalHeaders {
	return PESOptionalHeaders(ph[PESHeaderLen:])
}

func (ph PESHeaders) String() string {
	if !ph.Valid() {
		return "<invalid PES headers>"
	}
	var repr strings.Builder
	repr.WriteString(fmt.Sprintf("<StreamID=%s, PacketLength=%d, OptionalHeaders=", ph.StreamID(), ph.PacketLength()))
	opt := ph.OptionalHeaders()
	if opt.Valid() {
		repr.WriteString(opt.String())
	} else {
		repr.WriteString("<invalid>")
	}
	repr.WriteString(">")
	return repr.String()
}

func (ph PESHeaders) GoString() string {
	if !ph.Valid() {
		return "<invalid PES headers>"
	}
	var repr strings.Builder
	repr.WriteString(fmt.Sprintf("<StreamID=%#v, PacketLength=%d, OptionalHeaders=", ph.StreamID(), ph.PacketLength()))
	opt := ph.OptionalHeaders()
	if opt.Valid() {
		repr.WriteString(opt.GoString())
	} else {
		repr.WriteString("<invalid>")
	}
	repr.WriteString(">")
	return repr.String()
}

type StreamID byte

func (sid StreamID) StreamType() StreamType {
	switch {
	case sid >= 0xC0 && sid <= 0xDF:
		return StreamTypeAudio
	case sid >= 0xE0 && sid <= 0xEF:
		return StreamTypeVideo
	case sid == 0xBA: // seen in the wild
		return StreamTypeSubtitle
	default:
		return StreamTypeUnknown
	}
}

func (sid StreamID) String() string {
	return fmt.Sprintf("%02X", byte(sid))
}

func (sid StreamID) GoString() string {
	return fmt.Sprintf("%02X (%s)", byte(sid), sid.StreamType())
}

type StreamType uint8

const (
	StreamTypeUnknown StreamType = iota
	StreamTypeVideo
	StreamTypeAudio
	StreamTypeSubtitle
)

func (st StreamType) String() string {
	switch st {
	case StreamTypeUnknown:
		return "Unknown"
	case StreamTypeVideo:
		return "Video"
	case StreamTypeAudio:
		return "Audio"
	case StreamTypeSubtitle:
		return "Subtitle"
	default:
		return "Invalid"
	}
}

// https://en.wikipedia.org/wiki/Packetized_elementary_stream#Optional_PES_header
type PESOptionalHeaders [PESOptionalHeadersLen]byte

// Valid checks if the optional header marker is present at the start of the optional headers.
func (poh PESOptionalHeaders) Valid() bool {
	return poh[0]>>6 == PESOptionalHeadersMarker
}

// ScramblingControl returns the scrambling value. Check the related constants.
func (poh PESOptionalHeaders) ScramblingControl() ScramblingControl {
	return ScramblingControl((poh[0] & 0b00110000) >> 4)
}

// Priority returns true if the priority bit is set to 1, indicating that the PES packet has higher priority than other packets in the same PID. false = Normal priority, true = High priority.
func (poh PESOptionalHeaders) HighPriority() bool {
	return (poh[0]&0b00001000)>>3 == 1
}

// DataAligned returns true if the data alignment indicator is set to 1, indicating that the payload starts with a byte-aligned elementary stream. false = Not aligned, true = Aligned.
func (poh PESOptionalHeaders) DataAligned() bool {
	return (poh[0]&0b00000100)>>2 == 1
}

// Copyright returns true if the copyright bit is set to 1, indicating that the PES packet contains copyrighted material. false = Not copyrighted, true = Copyrighted.
func (poh PESOptionalHeaders) Copyright() bool {
	return (poh[0]&0b00000010)>>1 == 1
}

// Original returns true if the original bit is set to 1, indicating that the PES packet contains original material. false = Copy, true = Original.
func (poh PESOptionalHeaders) Original() bool {
	return poh[0]&0b00000001 == 1
}

func (poh PESOptionalHeaders) PTSDTSPresence() PTSDTSPresence {
	return PTSDTSPresence(poh[1] >> 6)
}

func (poh PESOptionalHeaders) PTSPresent() bool {
	return poh.PTSDTSPresence()&JustPTS != 0
}

func (poh PESOptionalHeaders) DTSPresent() bool {
	return poh.PTSDTSPresence()&JustDTS != 0
}

func (poh PESOptionalHeaders) ESCR() bool {
	return (poh[1]&0b00100000)>>5 == 1
}

func (poh PESOptionalHeaders) ESRate() bool {
	return (poh[1]&0b00010000)>>4 == 1
}

func (poh PESOptionalHeaders) DSMTrickMode() bool {
	return (poh[1]&0b00001000)>>3 == 1
}

func (poh PESOptionalHeaders) AdditionalCopyInfo() bool {
	return (poh[1]&0b00000100)>>2 == 1
}

func (poh PESOptionalHeaders) CRC() bool {
	return (poh[1]&0b00000010)>>1 == 1
}

func (poh PESOptionalHeaders) Extension() bool {
	return poh[1]&0b00000001 == 1
}

func (poh PESOptionalHeaders) RemainingHeaderLength() int64 {
	if poh.Valid() {
		fmt.Printf("Remaining header lenght: %08b\n", poh[2])
		return int64(poh[2])
	}
	return 0
}

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (poh PESOptionalHeaders) String() string {
	if !poh.Valid() {
		return "<invalid PES optional headers>"
	}
	return fmt.Sprintf("<ScramblingControl: %s | HighPriority: %v | DataAligned: %v | Copyright: %v | Original: %v | PTSDTSPresence: %s | ESCR: %v | ESRate: %v | DSMTrickMode: %v | AdditionalCopyInfo: %v | CRC: %v | Extension: %v | RemainingHeaderLength: %d>",
		poh.ScramblingControl(), poh.HighPriority(), poh.DataAligned(), poh.Copyright(), poh.Original(),
		poh.PTSDTSPresence(), poh.ESCR(), poh.ESRate(), poh.DSMTrickMode(), poh.AdditionalCopyInfo(), poh.CRC(), poh.Extension(),
		poh.RemainingHeaderLength(),
	)
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (poh PESOptionalHeaders) GoString() string {
	return fmt.Sprintf("%08b %08b %08b", poh[0], poh[1], poh[2])
}

type ScramblingControl byte

// https://patents.google.com/patent/WO2010000692A1/en
const (
	ScramblingControlNotScrambled ScramblingControl = 0b00
	ScramblingControlReserved     ScramblingControl = 0b01 // Reserved as per standard, should not be used.
	ScramblingControlEvenKey      ScramblingControl = 0b10
	ScramblingControlOddKey       ScramblingControl = 0b11
)

func (sc ScramblingControl) String() string {
	switch sc {
	case ScramblingControlNotScrambled:
		return "Not scrambled"
	case ScramblingControlReserved:
		return "Reserved"
	case ScramblingControlEvenKey:
		return "Even key"
	case ScramblingControlOddKey:
		return "Odd key"
	default:
		return "Unknown"
	}
}

func (sc ScramblingControl) GoString() string {
	return fmt.Sprintf("%s (%02b)", sc.String(), sc)
}

type PTSDTSPresence byte

const (
	NoPTSorDTSPresent PTSDTSPresence = 0b00
	JustPTS           PTSDTSPresence = 0b01 // Forbidden
	JustDTS           PTSDTSPresence = 0b10
	BothPTSandDTS     PTSDTSPresence = 0b11
)

func (ptd PTSDTSPresence) String() string {
	switch ptd {
	case NoPTSorDTSPresent:
		return "No PTS or DTS present"
	case JustPTS:
		return "Just PTS (forbidden)"
	case JustDTS:
		return "Just DTS"
	case BothPTSandDTS:
		return "Both PTS and DTS"
	default:
		return "Unknown"
	}
}

func (ptd PTSDTSPresence) GoString() string {
	return fmt.Sprintf("%s (%02b)", ptd.String(), ptd)
}
