package vobsub

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	PESExtensionMarker   = 0x2
	SubStreamIDBaseValue = 0x20
)

type PESPacket struct {
	Header  PESHeader
	Payload []byte
}

type PESHeader struct {
	StartCodeHeader StartCodeHeader
	PacketLength    [2]byte
	Extension       *PESExtension
	// Only for private streams (StreamID == 0xBD or 0xBF)
	SubStreamID SubStreamID
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

type SubStreamID [1]byte

func (ssid SubStreamID) SubtitleID() int {
	return int(ssid[0]) - SubStreamIDBaseValue
}

type PESExtension struct {
	Header [3]byte
	Data   PESExtensionData
}

// Valid checks if the optional header marker is present at the start of the optional headers.
func (poh PESExtension) Validate() error {
	if poh.Header[0]>>6 != PESExtensionMarker {
		return fmt.Errorf("invalid PES extension header marker: expected 0x%02X, got 0x%02X", PESExtensionMarker, poh.Header[0]>>6)
	}
	// Extension is validated during parsing with ParseExtensionData()
	return nil
}

// ScramblingControl returns the scrambling value. Check the related constants.
func (poh PESExtension) ScramblingControl() ScramblingControl {
	return ScramblingControl((poh.Header[0] & 0b00110000) >> 4)
}

// Priority returns true if the priority bit is set to 1, indicating that the PES packet has higher priority than other packets in the same PID. false = Normal priority, true = High priority.
func (poh PESExtension) HighPriority() bool {
	return (poh.Header[0]&0b00001000)>>3 == 1
}

// DataAligned returns true if the data alignment indicator is set to 1, indicating that the payload starts with a byte-aligned elementary stream. false = Not aligned, true = Aligned.
func (poh PESExtension) DataAligned() bool {
	return (poh.Header[0]&0b00000100)>>2 == 1
}

// Copyright returns true if the copyright bit is set to 1, indicating that the PES packet contains copyrighted material. false = Not copyrighted, true = Copyrighted.
func (poh PESExtension) Copyright() bool {
	return (poh.Header[0]&0b00000010)>>1 == 1
}

// Original returns true if the original bit is set to 1, indicating that the PES packet contains original material. false = Copy, true = Original.
func (poh PESExtension) Original() bool {
	return poh.Header[0]&0b00000001 == 1
}

func (poh PESExtension) PTSDTSPresence() PTSDTSPresence {
	return PTSDTSPresence(poh.Header[1] >> 6)
}

func (poh PESExtension) PTSPresent() bool {
	return poh.PTSDTSPresence()&JustPTS != 0
}

func (poh PESExtension) DTSPresent() bool {
	return poh.PTSDTSPresence()&JustDTS != 0
}

// ESCR ElementaryStreamClockReference
func (poh PESExtension) ESCRPresent() bool {
	return (poh.Header[1]&0b00100000)>>5 == 1
}

func (poh PESExtension) ESRatePresent() bool {
	return (poh.Header[1]&0b00010000)>>4 == 1
}

func (poh PESExtension) DSMTrickMode() bool {
	return (poh.Header[1]&0b00001000)>>3 == 1
}

func (poh PESExtension) AdditionalCopyInfoPresent() bool {
	return (poh.Header[1]&0b00000100)>>2 == 1
}

func (poh PESExtension) CRCPresent() bool {
	return (poh.Header[1]&0b00000010)>>1 == 1
}

func (poh PESExtension) SecondExtensionPresent() bool {
	return poh.Header[1]&0b00000001 == 1
}

func (poh PESExtension) RemainingHeaderLength() int64 {
	return int64(poh.Header[2])
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
		poh.PTSDTSPresence(), poh.ESCRPresent(), poh.ESRatePresent(), poh.DSMTrickMode(), poh.AdditionalCopyInfoPresent(), poh.CRCPresent(), poh.SecondExtensionPresent(),
		poh.RemainingHeaderLength(),
	)
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (poh PESExtension) GoString() string {
	return fmt.Sprintf("%08b %08b %08b", poh.Header[0], poh.Header[1], poh.Header[2]) // TODO data
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
	JustDTS           PTSDTSPresence = 0b01 // Forbidden
	JustPTS           PTSDTSPresence = 0b10
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

func (pesh PESHeader) ParseExtensionData(data []byte) (err error) {
	// checks
	if pesh.Extension == nil {
		err = errors.New("no extension headers found")
		return
	}
	if len(data) != int(pesh.Extension.RemainingHeaderLength()) {
		err = fmt.Errorf("received data len (%d) does not match expected len (%d)",
			len(data), pesh.Extension.RemainingHeaderLength())
		return
	}
	// Prepare
	index := 0
	// PTSDTS
	if pesh.Extension.PTSDTSPresence()&JustPTS == JustPTS {
		PTSSize := 5
		pesh.Extension.Data.PTS = make([]byte, PTSSize)
		for i := range PTSSize {
			pesh.Extension.Data.PTS[i] = data[index+i]
		}
		// done
		index += PTSSize
		// fmt.Println("PTS extracted !")
	}
	if pesh.Extension.PTSDTSPresence()&JustDTS == JustDTS {
		DTSSize := 5
		pesh.Extension.Data.DTS = make([]byte, DTSSize)
		for i := range DTSSize {
			pesh.Extension.Data.DTS[i] = data[index+i]
		}
		// done
		index += DTSSize
		// fmt.Println("DTS extracted !")
	}
	// ESCR
	if pesh.Extension.ESCRPresent() {
		ESCRSize := 6
		pesh.Extension.Data.ESCR = make([]byte, ESCRSize)
		for i := range ESCRSize {
			pesh.Extension.Data.ESCR[i] = data[index+i]
		}
		// done
		index += ESCRSize
		// fmt.Println("ESCR extracted !")
	}
	// ES rate
	if pesh.Extension.ESRatePresent() {
		ESRateSize := 3
		pesh.Extension.Data.ESRate = make([]byte, ESRateSize)
		for i := range ESRateSize {
			pesh.Extension.Data.ESRate[i] = data[index+i]
		}
		// done
		index += ESRateSize
		// fmt.Println("ESRate extracted !")
	}
	// additional copy info
	if pesh.Extension.AdditionalCopyInfoPresent() {
		// Check fixed bit
		if data[index]&0b10000000 != 0b10000000 {
			err = errors.New("additionnal copy info fixed bit is invalid")
			return
		}
		// Extract value
		value := data[index] & 0b01111111
		pesh.Extension.Data.AdditionalCopyInfo = &value
		// done
		index++
		// fmt.Println("Additional Copy Info parsed !")
	}
	// PES CRC
	if pesh.Extension.CRCPresent() {
		CRCSize := 2
		pesh.Extension.Data.PreviousPacketCRC = make([]byte, CRCSize)
		for i := range CRCSize {
			pesh.Extension.Data.PreviousPacketCRC[i] = data[index+i]
		}
		// done
		index += CRCSize
		// fmt.Println("ESRate extracted !")
	}
	// PES extension flag
	if pesh.Extension.SecondExtensionPresent() {
		headers := data[index]
		index++
		// PES private data flag
		if headers&0b10000000 == 0b10000000 {
			privateDataSize := 16
			pesh.Extension.Data.PrivateData = make([]byte, privateDataSize)
			for i := range privateDataSize {
				pesh.Extension.Data.PrivateData[i] = data[index+i]
			}
			index += privateDataSize
			// fmt.Println("Private Data extracted !")
		}
		// pack header field flag
		if headers&0b01000000 == 0b01000000 {
			value := data[index]
			pesh.Extension.Data.PackHeaderField = &value
			// fmt.Println("PackHeader field flag set in the PES extension data: unsure of subsequent read") // mmm
			index++
		}
		// program packet sequence counter flag
		if headers&0b00100000 == 0b00100000 {
			programPacketSequenceCounterSize := 2
			pesh.Extension.Data.ProgramPacketSequenceCounter = make([]byte, programPacketSequenceCounterSize)
			for i := range programPacketSequenceCounterSize {
				pesh.Extension.Data.ProgramPacketSequenceCounter[i] = data[index+i]
			}
			index += programPacketSequenceCounterSize
			// fmt.Println("program packet sequence counter extracted !")
		}
		// P-STD buffer flag
		if headers&0b00010000 == 0b00010000 {
			PSTDSize := 2
			pesh.Extension.Data.PSTD = make([]byte, PSTDSize)
			for i := range PSTDSize {
				pesh.Extension.Data.PSTD[i] = data[index+i]
			}
			index += PSTDSize
			// fmt.Println("P-STD buffer extracted !")
		}
		// Fixed bytes
		if headers&0b00001110 != 0b00001110 {
			err = fmt.Errorf("PES second extension headers fixed bytes are invalid")
			return
		}
		// PES extension flag 2
		if headers&0b000000001 == 0b000000001 {
			// Parse headers
			PESExtensionSize := 2
			header := make([]byte, PESExtensionSize)
			for i := range PESExtensionSize {
				header[i] = data[index+i]
			}
			index += PESExtensionSize
			// Extract data
			additionnalDataLen := int(header[0] & 0b01111111)
			//// header[1] is reserved for futur use
			pesh.Extension.Data.PESExtensionSecond = make([]byte, additionnalDataLen)
			for i := range additionnalDataLen {
				pesh.Extension.Data.PSTD[i] = data[index+i]
			}
			index += additionnalDataLen
			// fmt.Println("PES Extension 2 data extracted !")
		}
	}
	// Check final padding
	for i := index; i < len(data); i++ {
		if data[i] != 0xFF {
			err = fmt.Errorf("invalid padding at the end of the extension data")
			return
		}
		// fmt.Println("valid padding yay")
	}
	return
}

type PESExtensionData struct {
	PTS                []byte // Presentation Time Stamp
	DTS                []byte // Decode Time Stamp
	ESCR               []byte // Elementary Stream Clock Reference (used if the stream and system levels are not synchronized (i.e. ESCR differs from SCR in the PACK header))
	ESRate             []byte // The rate at which data is delivered for this stream, in units of 50 bytes/second
	AdditionalCopyInfo *byte  // unknown format
	PreviousPacketCRC  []byte // The polynomial used is X(16) + X(12) + X(5) + 1
	// Second extension
	PrivateData                  []byte
	PackHeaderField              *byte
	ProgramPacketSequenceCounter []byte
	PSTD                         []byte
	PESExtensionSecond           []byte
}

func (pesed *PESExtensionData) Len() (length int) {
	length += len(pesed.DTS)
	length += len(pesed.DTS)
	length += len(pesed.ESCR)
	length += len(pesed.ESRate)
	if pesed.AdditionalCopyInfo != nil {
		length++
	}
	length += len(pesed.PreviousPacketCRC)
	length += len(pesed.PrivateData)
	if pesed.PackHeaderField != nil {
		length++
	}
	length += len(pesed.ProgramPacketSequenceCounter)
	length += len(pesed.PSTD)
	length += len(pesed.PESExtensionSecond)
	return
}

func (pesed *PESExtensionData) ComputePTS() (pts time.Duration) {
	if len(pesed.PTS) == 0 {
		return
	}
	var ticks uint64
	ticks |= (uint64(pesed.PTS[0]&0b00001110) >> 1) << 30
	ticks |= uint64(pesed.PTS[1]) << 22
	ticks |= (uint64(pesed.PTS[2]&0b11111110) >> 1) << 15
	ticks |= uint64(pesed.PTS[3]) << 7
	ticks |= uint64(pesed.PTS[4]&0b11111110) >> 1
	return time.Duration(ticks * uint64(time.Second) / PTSDTSClockFrequency)
}
