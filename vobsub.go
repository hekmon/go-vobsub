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

func ReadVobSub(subFile string) (err error) {
	// Parse IDX
	//// TODO
	// Open the binary sub file
	fd, err := os.Open(subFile)
	if err != nil {
		err = fmt.Errorf("failed to open subtitle file: %w", err)
		return
	}
	defer fd.Close()
	// // Parse each packet
	// packet1 := make([]byte, 0x000001000)
	// if _, err = fd.ReadAt(packet1, 0); err != nil {
	// 	err = fmt.Errorf("failed to read PES 1st packet: %w", err)
	// 	return
	// }
	// fmt.Printf("packet1: %08b\n", packet1) // Debugging line
	// fmt.Println()

	// if err = parseStream(fd, 0); err != nil {
	// 	return
	// }
	// fmt.Println()
	// if err = parseStream(fd, 0x000001000); err != nil {
	// 	return
	// }
	// fmt.Println()
	// if err = parseStream(fd, 0x000002800); err != nil {
	// 	return
	// }

	var (
		nextAt int64
		// packet PESPacket
	)
	for nextAt >= 0 {
		fmt.Println("next packet at cursor", nextAt)
		if _, nextAt, err = parsePacket(fd, nextAt); err != nil {
			err = fmt.Errorf("failed to parse packet: %w", err)
			return
		}
		fmt.Println()
	}
	return
}

func parsePacket(fd *os.File, currentPosition int64) (packet PESPacket, nextAt int64, err error) {
	// Read Start code and verify it is a pack header
	var (
		sch    StartCodeHeader
		nbRead int
	)
	if nbRead, err = fd.ReadAt(sch[:], currentPosition); err != nil {
		if errors.Is(err, io.EOF) {
			// strange but seen in the wild
			err = nil
			nextAt = -1
		} else {
			err = fmt.Errorf("failed to read start code header: %w", err)
		}
		return
	}
	currentPosition += int64(nbRead)
	if err = sch.Validate(); err != nil {
		err = fmt.Errorf("invalid MPEG header: %w", err)
		return
	}
	// Act depending on stream ID
	switch sch.StreamID() {
	case StreamIDPackHeader:
		if packet, nextAt, err = parsePackHeader(fd, currentPosition, sch); err != nil {
			err = fmt.Errorf("failed to parse Pack Header: %w", err)
			return
		}
		return
	case StreamIDPaddingStream:
		if nextAt, err = parsePaddingStream(fd, currentPosition, sch); err != nil {
			err = fmt.Errorf("failed to parse padding stream: %w", err)
			return
		}
		return
	case StreamIDProgramEnd:
		nextAt = stopFlagValue
		return
	default:
		err = fmt.Errorf("unexpected stream ID: %s", sch.StreamID())
		return
	}
}

func parsePackHeader(fd *os.File, currentPosition int64, sch StartCodeHeader) (packet PESPacket, nextPacketPosition int64, err error) {
	var nbRead int
	// Finish reading pack header
	ph := PackHeader{
		StartCodeHeader: sch,
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
	fmt.Println(ph.String())
	// fmt.Println(ph.GoString())
	// Next read the PES header
	var pes PESHeader
	if nbRead, err = fd.ReadAt(pes.StartCodeHeader[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = pes.StartCodeHeader.Validate(); err != nil {
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
	switch pes.StartCodeHeader.StreamID() {
	case StreamIDPrivateStream1:
		if packet, err = parsePESSubtitlePacket(fd, currentPosition, pes); err != nil {
			err = fmt.Errorf("failed to parse subtitle stream (private stream 1) packet: %w", err)
			return
		}
		return
	default:
		err = fmt.Errorf("unexpected PES Stream ID: %s", pes.StartCodeHeader.StreamID())
		return
	}
}

func parsePESSubtitlePacket(fd *os.File, currentPosition int64, preHeader PESHeader) (packet PESPacket, err error) {
	var nbRead int
	packet.Header = preHeader
	// Finish reading PES header
	//// 0xBD stream type has PES header extension, read it
	packet.Header.Extension = new(PESExtension)
	if nbRead, err = fd.ReadAt(packet.Header.Extension[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES extension header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Read PES Extension Data
	packet.Header.ExtensionData = make([]byte, packet.Header.Extension.RemainingHeaderLength())
	if nbRead, err = fd.ReadAt(packet.Header.ExtensionData, currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES extension data: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Read sub stream id for private streams (we are one, checked earlier we are PESStreamIDPrivateStream1)
	if nbRead, err = fd.ReadAt(packet.Header.SubStreamID[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read sub stream id: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Headers done
	fmt.Println(packet.Header.String())
	// fmt.Println(packet.Header.GoString())
	// Payload
	packet.Payload = make([]byte, packet.Header.GetPacketLength()-len(*packet.Header.Extension)-len(packet.Header.ExtensionData)-len(packet.Header.SubStreamID))
	if _, err = fd.ReadAt(packet.Payload, currentPosition); err != nil {
		err = fmt.Errorf("failed to read the payload: %w", err)
		return
	}
	return
}

func parsePaddingStream(fd *os.File, currentPosition int64, sch StartCodeHeader) (nextPacketPosition int64, err error) {
	var nbRead int
	// Read the PES header used in padding
	pes := PESHeader{
		StartCodeHeader: sch,
	}
	if nbRead, err = fd.ReadAt(pes.PacketLength[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES Packet Lenght header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	nextPacketPosition = currentPosition + int64(pes.GetPacketLength()) // packet len is all data after the header ending with the data len
	//
	fmt.Println("Padding len:", pes.GetPacketLength())
	return
}
