package vobsub

import (
	"fmt"
	"os"
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

	var nextAt int64
	for {
		if nextAt, err = parseStream(fd, nextAt); err != nil {
			return
		}
		fmt.Println("next packet at cursor", nextAt)
		fmt.Println()
	}
}

func parseStream(fd *os.File, startAt int64) (nextAt int64, err error) {
	// Read Start code and verify it is a pack header
	cursor := startAt
	var (
		sch    StartCodeHeader
		nbRead int
	)
	if nbRead, err = fd.ReadAt(sch[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	cursor += int64(nbRead)
	if err = sch.Validate(); err != nil {
		err = fmt.Errorf("invalid MPEG header: %w", err)
		return
	}
	if sch.StreamID() != 0xBA {
		err = fmt.Errorf("unexpected stream ID: %s", sch.StreamID())
		return
	}
	// We do have a pack header, read it fully
	ph := PackHeader{
		StartCodeHeader: sch,
	}
	if nbRead, err = fd.ReadAt(ph.Remaining[:], cursor); err != nil {
		err = fmt.Errorf("failed to read pack header: %w", err)
		return
	}
	cursor += int64(nbRead)
	if err = ph.Validate(); err != nil {
		err = fmt.Errorf("invalid pack header: %w", err)
		return
	}
	cursor += ph.StuffingBytesLength()
	fmt.Println(ph.String())
	fmt.Println(ph.GoString())
	// Next read the PES header
	var pes PESHeader
	if nbRead, err = fd.ReadAt(pes.StartCodeHeader[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	cursor += int64(nbRead)
	if err = pes.StartCodeHeader.Validate(); err != nil {
		err = fmt.Errorf("invalid PES header: invalid start code: %w", err)
		return
	}
	if pes.StartCodeHeader.StreamID() != PrivateStream1ID {
		// We expect a Private stream 1 StreamID (for subtitles)
		err = fmt.Errorf("unexpected PES stream ID: 0x%02X (expecting 0x%02x)",
			byte(pes.StartCodeHeader.StreamID()), PrivateStream1ID)
		return
	}
	//// Read PES packet length
	if nbRead, err = fd.ReadAt(pes.PacketLength[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES Packet Lenght header: %w", err)
		return
	}
	cursor += int64(nbRead)
	nextAt = cursor + int64(pes.GetPacketLength()) // packet len is all data after the header ending with the data len
	//// 0xBD stream type has PES header extension, read it
	pes.Extension = new(PESExtension)
	if nbRead, err = fd.ReadAt(pes.Extension[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES extension header: %w", err)
		return
	}
	cursor += int64(nbRead)
	//// Read PES Extension Data
	pes.ExtensionData = make([]byte, pes.Extension.RemainingHeaderLength())
	if nbRead, err = fd.ReadAt(pes.ExtensionData, cursor); err != nil {
		err = fmt.Errorf("failed to read PES extension data: %w", err)
		return
	}
	cursor += int64(nbRead)
	//// Read sub stream id for private streams (we are one, checked earlier we are PESStreamIDPrivateStream1)
	if nbRead, err = fd.ReadAt(pes.SubStreamID[:], cursor); err != nil {
		err = fmt.Errorf("failed to read sub stream id: %w", err)
		return
	}
	cursor += int64(nbRead)
	//// Headers done
	fmt.Println(pes.String())
	fmt.Println(pes.GoString())
	// Payload
	buffer := make([]byte, pes.GetPacketLength()-len(*pes.Extension)-len(pes.ExtensionData)-len(pes.SubStreamID))
	if nbRead, err = fd.ReadAt(buffer, cursor); err != nil {
		err = fmt.Errorf("failed to read the payload: %w", err)
		return
	}
	fmt.Println("Payload has", len(buffer), "bytes")
	cursor += int64(nbRead)
	if cursor != nextAt {
		err = fmt.Errorf("cursor is misaligned after reading the packet")
	}
	return
}
