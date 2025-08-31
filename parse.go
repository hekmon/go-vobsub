package vobsub

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	stopFlagValue = -1
)

func parsePacket(fd *os.File, currentPosition int64) (packet PESPacket, nextAt int64, err error) {
	// Read Start code and verify it is a pack header
	var (
		mph    MPEGHeader
		nbRead int
	)
	if nbRead, err = fd.ReadAt(mph[:], currentPosition); err != nil {
		if errors.Is(err, io.EOF) {
			// strange but seen in the wild
			err = nil
			nextAt = stopFlagValue
		} else {
			err = fmt.Errorf("failed to read start code header: %w", err)
		}
		return
	}
	currentPosition += int64(nbRead)
	if err = mph.Validate(); err != nil {
		err = fmt.Errorf("invalid MPEG header: %w", err)
		return
	}
	// Act depending on stream ID
	switch mph.StreamID() {
	case StreamIDPackHeader:
		if packet, nextAt, err = parsePackHeader(fd, currentPosition, mph); err != nil {
			err = fmt.Errorf("failed to parse Pack Header: %w", err)
			return
		}
		return
	case StreamIDPaddingStream:
		if nextAt, err = parsePaddingStream(fd, currentPosition, mph); err != nil {
			err = fmt.Errorf("failed to parse padding stream: %w", err)
			return
		}
		return
	case StreamIDProgramEnd:
		nextAt = stopFlagValue
		return
	default:
		err = fmt.Errorf("unexpected stream ID: %s", mph.StreamID())
		return
	}
}

func parsePackHeader(fd *os.File, currentPosition int64, mph MPEGHeader) (packet PESPacket, nextPacketPosition int64, err error) {
	var nbRead int
	// Finish reading pack header
	ph := PackHeader{
		MPH: mph,
	}
	if nbRead, err = fd.ReadAt(ph.Remaining[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read pack header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = ph.Validate(); err != nil {
		err = fmt.Errorf("invalid pack header: %w", err)
		return
	}
	currentPosition += ph.StuffingBytesLength()
	// fmt.Println(ph.String())
	// fmt.Println(ph.GoString())
	// Next read the PES header
	var pes PESHeader
	if nbRead, err = fd.ReadAt(pes.MPH[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = pes.MPH.Validate(); err != nil {
		err = fmt.Errorf("invalid PES header: invalid start code: %w", err)
		return
	}
	if nbRead, err = fd.ReadAt(pes.PacketLength[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES Packet Lenght header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	nextPacketPosition = currentPosition + int64(pes.GetPacketLength()) // packet len is all data after the header ending with the data len
	// Continue depending on stream ID
	switch pes.MPH.StreamID() {
	case StreamIDPrivateStream1:
		if packet, err = parsePESPrivateStream1Packet(fd, currentPosition, pes); err != nil {
			err = fmt.Errorf("failed to parse subtitle stream (private stream 1) packet: %w", err)
			return
		}
		return
	default:
		err = fmt.Errorf("unexpected PES Stream ID: %s", pes.MPH.StreamID())
		return
	}
}

func parsePESPrivateStream1Packet(fd *os.File, currentPosition int64, preHeader PESHeader) (packet PESPacket, err error) {
	var nbRead int
	packet.Header = preHeader
	// Finish reading PES header
	//// 0xBD stream type has PES header extension, read it
	packet.Header.Extension = new(PESExtension)
	if nbRead, err = fd.ReadAt(packet.Header.Extension.Header[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES extension header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Read PES Extension Data
	extensionData := make([]byte, packet.Header.Extension.RemainingHeaderLength())
	if nbRead, err = fd.ReadAt(extensionData, currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES extension data: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = packet.Header.ParseExtensionData(extensionData); err != nil {
		err = fmt.Errorf("failed to parse extension header data: %w", err)
		return
	}
	//// Read sub stream id for private streams
	if nbRead, err = fd.ReadAt(packet.Header.SubStreamID[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read sub stream id: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Headers done
	// fmt.Println(packet.Header.String())
	// fmt.Println(packet.Header.GoString())
	// Payload
	payloadLen := packet.Header.GetPacketLength() - len(packet.Header.Extension.Header) - len(extensionData) - len(packet.Header.SubStreamID)
	packet.Payload = make([]byte, payloadLen)
	if _, err = fd.ReadAt(packet.Payload, currentPosition); err != nil {
		err = fmt.Errorf("failed to read the payload: %w", err)
		return
	}
	return
}

func parsePaddingStream(fd *os.File, currentPosition int64, mph MPEGHeader) (nextPacketPosition int64, err error) {
	var nbRead int
	// Read the PES header used in padding
	pes := PESHeader{
		MPH: mph,
	}
	if nbRead, err = fd.ReadAt(pes.PacketLength[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES Packet Lenght header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	nextPacketPosition = currentPosition + int64(pes.GetPacketLength()) // packet len is all data after the header ending with the data len
	// // Debug
	// fmt.Println("Padding len:", pes.GetPacketLength())
	// buffer := make([]byte, pes.GetPacketLength())
	// if _, err = fd.ReadAt(buffer, currentPosition); err != nil {
	// 	err = fmt.Errorf("failed to read the payload: %w", err)
	// 	return
	// }
	// for _, b := range buffer {
	// 	fmt.Printf("0x%02x ", b) // all should be 0xff
	// }
	// fmt.Println()
	return
}

// parseExtensionData is a low level parsing function, used by the PESHeader ParseExtensionData() high level method
func parsePESExtensionData(extensionHeaders *PESExtension, data []byte) (index int, err error) {
	if extensionHeaders == nil {
		// no extension but high level caller may want to check padding after so not returning an error
		return
	}
	if len(data) != extensionHeaders.RemainingHeaderLength() {
		err = fmt.Errorf("received data len (%d) does not match expected len (%d)",
			len(data), extensionHeaders.RemainingHeaderLength())
		return
	}
	// PTSDTS
	if extensionHeaders.PTSDTSPresence()&JustPTS == JustPTS {
		PTSSize := 5
		extensionHeaders.Data.PTS = make([]byte, PTSSize)
		for i := range PTSSize {
			extensionHeaders.Data.PTS[i] = data[index+i]
		}
		// done
		index += PTSSize
		// fmt.Println("PTS extracted !")
	}
	if extensionHeaders.PTSDTSPresence()&JustDTS == JustDTS {
		DTSSize := 5
		extensionHeaders.Data.DTS = make([]byte, DTSSize)
		for i := range DTSSize {
			extensionHeaders.Data.DTS[i] = data[index+i]
		}
		// done
		index += DTSSize
		// fmt.Println("DTS extracted !")
	}
	// ESCR
	if extensionHeaders.ESCRPresent() {
		ESCRSize := 6
		extensionHeaders.Data.ESCR = make([]byte, ESCRSize)
		for i := range ESCRSize {
			extensionHeaders.Data.ESCR[i] = data[index+i]
		}
		// done
		index += ESCRSize
		// fmt.Println("ESCR extracted !")
	}
	// ES rate
	if extensionHeaders.ESRatePresent() {
		ESRateSize := 3
		extensionHeaders.Data.ESRate = make([]byte, ESRateSize)
		for i := range ESRateSize {
			extensionHeaders.Data.ESRate[i] = data[index+i]
		}
		// done
		index += ESRateSize
		// fmt.Println("ESRate extracted !")
	}
	// additional copy info
	if extensionHeaders.AdditionalCopyInfoPresent() {
		// Check fixed bit
		if data[index]&0b10000000 != 0b10000000 {
			err = errors.New("additionnal copy info fixed bit is invalid")
			return
		}
		// Extract value
		value := data[index] & 0b01111111
		extensionHeaders.Data.AdditionalCopyInfo = &value
		// done
		index++
		// fmt.Println("Additional Copy Info parsed !")
	}
	// PES CRC
	if extensionHeaders.CRCPresent() {
		CRCSize := 2
		extensionHeaders.Data.PreviousPacketCRC = make([]byte, CRCSize)
		for i := range CRCSize {
			extensionHeaders.Data.PreviousPacketCRC[i] = data[index+i]
		}
		// done
		index += CRCSize
		// fmt.Println("ESRate extracted !")
	}
	// PES extension flag
	if !extensionHeaders.SecondExtensionPresent() {
		return
	}
	headers := data[index]
	index++
	// PES private data flag
	if headers&0b10000000 == 0b10000000 {
		privateDataSize := 16
		extensionHeaders.Data.PrivateData = make([]byte, privateDataSize)
		for i := range privateDataSize {
			extensionHeaders.Data.PrivateData[i] = data[index+i]
		}
		index += privateDataSize
		// fmt.Println("Private Data extracted !")
	}
	// pack header field flag
	if headers&0b01000000 == 0b01000000 {
		value := data[index]
		extensionHeaders.Data.PackHeaderField = &value
		// fmt.Println("PackHeader field flag set in the PES extension data: unsure of subsequent read") // mmm
		index++
	}
	// program packet sequence counter flag
	if headers&0b00100000 == 0b00100000 {
		programPacketSequenceCounterSize := 2
		extensionHeaders.Data.ProgramPacketSequenceCounter = make([]byte, programPacketSequenceCounterSize)
		for i := range programPacketSequenceCounterSize {
			extensionHeaders.Data.ProgramPacketSequenceCounter[i] = data[index+i]
		}
		index += programPacketSequenceCounterSize
		// fmt.Println("program packet sequence counter extracted !")
	}
	// P-STD buffer flag
	if headers&0b00010000 == 0b00010000 {
		PSTDSize := 2
		extensionHeaders.Data.PSTD = make([]byte, PSTDSize)
		for i := range PSTDSize {
			extensionHeaders.Data.PSTD[i] = data[index+i]
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
		extensionHeaders.Data.PESExtensionSecond = make([]byte, additionnalDataLen)
		for i := range additionnalDataLen {
			extensionHeaders.Data.PSTD[i] = data[index+i]
		}
		index += additionnalDataLen
		// fmt.Println("PES Extension 2 data extracted !")
	}
	return
}

func parseSubtitle(packet PESPacket) (subtitle Subtitle, err error) {
	// Read the size first
	size := int(packet.Payload[0])<<8 | int(packet.Payload[1])
	// fmt.Printf("Packet len: 0b%08b 0b%08b -> %d\n", packet.Payload[0], packet.Payload[1], size)
	if len(packet.Payload) != size {
		err = fmt.Errorf("the read packet size (%d) does not match the received packet length (%d)", size, len(packet.Payload))
		return
	}
	// Read the data packet size to split the data and the control sequences
	dataSize := int(packet.Payload[2])<<8 | int(packet.Payload[3])
	// fmt.Printf("Data Packet len: 0b%08b 0b%08b -> %d\n", packet.Payload[2], packet.Payload[3], dataSize)
	if dataSize > len(packet.Payload)-2 {
		err = fmt.Errorf("the read data packet size (%d) is greater than the total packet size (%d)", size, len(packet.Payload))
		return
	}
	// Split
	subtitle.data = packet.Payload[2+2 : 2+dataSize] // need to check this
	ctrlseqs := packet.Payload[2+dataSize:]
	fmt.Printf("Data len is %d and ctrl seq len is %d\n", len(subtitle.data), len(ctrlseqs))
	// Parse control sequences
	err = parseCTRLSeq(ctrlseqs)
	// Decode image
	//// TODO
	return
}

func parseCTRLSeq(sequences []byte) (err error) {
	return
}
