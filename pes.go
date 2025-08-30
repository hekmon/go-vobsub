package vobsub

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	PESPacketLen                   = 2
	PESExtensionLen                = 3
	PESExtensionMarker             = 0x2
	PESPrivateStreamSubStreamIDLen = 1
)

type PESPacket struct {
	Header  PESHeader
	Payload []byte
}

type PESHeader struct {
	StartCodeHeader StartCodeHeader
	PacketLength    [PESPacketLen]byte
	// Extension
	Extension     *PESExtension
	ExtensionData []byte
	// Only for private streams (StreamID == 0xBD or 0xBF)
	SubStreamID [PESPrivateStreamSubStreamIDLen]byte
}

func (pesh PESHeader) Validate() (err error) {
	if err = pesh.StartCodeHeader.Validate(); err != nil {
		return fmt.Errorf("invalid PES header: invalid start code: %w", err)
	}
	if pesh.Extension != nil {
		if err = pesh.Extension.Validate(); err != nil {
			return fmt.Errorf("invalid PES header: invalid extension: %w", err)
		}
	}
	return nil
}

func (pesh PESHeader) GetPacketLength() int {
	return int(binary.BigEndian.Uint16(pesh.PacketLength[:]))
}

func (pesh PESHeader) String() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("PESHeader{StartCodeHeader: %s, PacketLength: %d, Extension: ",
		pesh.StartCodeHeader, pesh.GetPacketLength()))
	if pesh.Extension != nil {
		builder.WriteString(pesh.Extension.String())
	} else {
		builder.WriteString("<nil>")
	}
	if pesh.StartCodeHeader.StreamID() == StreamIDPrivateStream1 ||
		pesh.StartCodeHeader.StreamID() == StreamIDPrivateStream2 {
		// Private stream, print sub stream id too
		builder.WriteString(fmt.Sprintf(", SubStreamID: 0x%02x", pesh.SubStreamID[0]))
	}
	builder.WriteString("}")
	return builder.String()
}

func (pesh PESHeader) GoString() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("PESHeader{StartCodeHeader: %#v, PacketLength: %08b%08b, Extension: ",
		pesh.StartCodeHeader, byte(pesh.PacketLength[0]), byte(pesh.PacketLength[1])))
	if pesh.Extension != nil {
		builder.WriteString(pesh.Extension.GoString())
	} else {
		builder.WriteString("<nil>")
	}
	if pesh.StartCodeHeader.StreamID() == StreamIDPrivateStream1 ||
		pesh.StartCodeHeader.StreamID() == StreamIDPrivateStream2 {
		// Private stream, print sub stream id too
		builder.WriteString(fmt.Sprintf(", SubStreamID: %08b", pesh.SubStreamID[0]))
	}
	builder.WriteString("}")
	return builder.String()
}

// https://en.wikipedia.org/wiki/Packetized_elementary_stream#Optional_PES_header
type PESExtension [PESExtensionLen]byte

// Valid checks if the optional header marker is present at the start of the optional headers.
func (poh PESExtension) Validate() error {
	if poh[0]>>6 != PESExtensionMarker {
		return fmt.Errorf("invalid PES extension header marker: expected 0x%02X, got 0x%02X", PESExtensionMarker, poh[0]>>6)
	}
	// Calculate extra data len
	var expectedDataLen int64
	if poh.DTSPresent() {
		expectedDataLen += 5
	}
	if poh.PTSPresent() {
		expectedDataLen += 5
	}
	if poh.ElementaryStreamClockReferencePresent() {
		expectedDataLen += 6
	}
	if poh.ESRatePresent() {
		expectedDataLen += 3
	}
	if poh.AdditionalCopyInfoPresent() {
		expectedDataLen += 1
	}
	if poh.CRCPresent() {
		expectedDataLen += 2
	}
	if poh.SecondExtensionPresent() {
		expectedDataLen += 1 // headers
		// TODO check flags for second extension and calculate length
	}
	if poh.RemainingHeaderLength() != expectedDataLen {
		return fmt.Errorf("invalid PES extension header length: expected %d, got %d", expectedDataLen, poh.RemainingHeaderLength())
	}
	return nil
}

// ScramblingControl returns the scrambling value. Check the related constants.
func (poh PESExtension) ScramblingControl() ScramblingControl {
	return ScramblingControl((poh[0] & 0b00110000) >> 4)
}

// Priority returns true if the priority bit is set to 1, indicating that the PES packet has higher priority than other packets in the same PID. false = Normal priority, true = High priority.
func (poh PESExtension) HighPriority() bool {
	return (poh[0]&0b00001000)>>3 == 1
}

// DataAligned returns true if the data alignment indicator is set to 1, indicating that the payload starts with a byte-aligned elementary stream. false = Not aligned, true = Aligned.
func (poh PESExtension) DataAligned() bool {
	return (poh[0]&0b00000100)>>2 == 1
}

// Copyright returns true if the copyright bit is set to 1, indicating that the PES packet contains copyrighted material. false = Not copyrighted, true = Copyrighted.
func (poh PESExtension) Copyright() bool {
	return (poh[0]&0b00000010)>>1 == 1
}

// Original returns true if the original bit is set to 1, indicating that the PES packet contains original material. false = Copy, true = Original.
func (poh PESExtension) Original() bool {
	return poh[0]&0b00000001 == 1
}

func (poh PESExtension) PTSDTSPresence() PTSDTSPresence {
	return PTSDTSPresence(poh[1] >> 6)
}

func (poh PESExtension) PTSPresent() bool {
	return poh.PTSDTSPresence()&JustPTS != 0
}

func (poh PESExtension) DTSPresent() bool {
	return poh.PTSDTSPresence()&JustDTS != 0
}

func (poh PESExtension) ElementaryStreamClockReferencePresent() bool {
	return (poh[1]&0b00100000)>>5 == 1
}

func (poh PESExtension) ESRatePresent() bool {
	return (poh[1]&0b00010000)>>4 == 1
}

func (poh PESExtension) DSMTrickMode() bool {
	return (poh[1]&0b00001000)>>3 == 1
}

func (poh PESExtension) AdditionalCopyInfoPresent() bool {
	return (poh[1]&0b00000100)>>2 == 1
}

func (poh PESExtension) CRCPresent() bool {
	return (poh[1]&0b00000010)>>1 == 1
}

func (poh PESExtension) SecondExtensionPresent() bool {
	return poh[1]&0b00000001 == 1
}

func (poh PESExtension) RemainingHeaderLength() int64 {
	return int64(poh[2])
}

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (poh PESExtension) String() string {
	if err := poh.Validate(); err != nil {
		return fmt.Sprintf("<invalid PES optional headers: %s>", err)
	}
	return fmt.Sprintf("PESExtension{ScramblingControl: %#v, HighPriority: %v, DataAligned: %v, Copyright: %v, Original: %v, PTSDTSPresence: %#v, ElementaryStreamClockReference: %v, ESRate: %v, DSMTrickMode: %v, AdditionalCopyInfo: %v, CRC: %v, SecondExtension: %v, RemainingHeaderLength: %d}",
		poh.ScramblingControl(), poh.HighPriority(), poh.DataAligned(), poh.Copyright(), poh.Original(),
		poh.PTSDTSPresence(), poh.ElementaryStreamClockReferencePresent(), poh.ESRatePresent(), poh.DSMTrickMode(), poh.AdditionalCopyInfoPresent(), poh.CRCPresent(), poh.SecondExtensionPresent(),
		poh.RemainingHeaderLength(),
	)
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (poh PESExtension) GoString() string {
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

type PESExtensionData struct{}

func (pesh PESHeader) ParseExtensionData() (ped PESExtensionData, err error) {
	if pesh.Extension == nil {
		err = errors.New("no extension headers found")
		return
	}
	// TODO: Implement parsing of extension data
	// https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
	// PTS
	// DTS
	// ESCR
	// ES rate
	// additional copy info
	// PES CRC
	// PES extension flag
	//// PES private data flag
	//// pack header field flag
	//// program packet sequence counter flag
	//// P-STD buffer flag
	//// PES extension flag 2
	return
}
