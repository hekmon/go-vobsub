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
	if err = parseStream(fd, 0); err != nil {
		return
	}
	fmt.Println()
	if err = parseStream(fd, 0x000001000); err != nil {
		return
	}
	fmt.Println()
	if err = parseStream(fd, 0x000002800); err != nil {
		return
	}
	return
}

func parseStream(fd *os.File, startAt int64) (err error) {
	// Read Start code and verify it is a pack header
	cursor := startAt
	var sch StartCodeHeader
	if _, err = fd.ReadAt(sch[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	cursor += int64(len(sch))
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
	if _, err = fd.ReadAt(ph.Remaining[:], cursor); err != nil {
		err = fmt.Errorf("failed to read pack header: %w", err)
		return
	}
	cursor += int64(len(ph.Remaining))
	if err = ph.Validate(); err != nil {
		err = fmt.Errorf("invalid pack header: %w", err)
		return
	}
	cursor += ph.StuffingBytesLength()
	// Next read the PES header
	var pes PESHeader
	if _, err = fd.ReadAt(pes.StartCodeHeader[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	cursor += int64(len(pes.StartCodeHeader))
	if err = pes.StartCodeHeader.Validate(); err != nil {
		err = fmt.Errorf("invalid PES header: invalid start code: %w", err)
		return
	}
	if pes.StartCodeHeader.StreamID() != 0xBD {
		// We expect a Private stream 1 StreamID (for subtitles)
		err = fmt.Errorf("unexpected PES stream ID: 0x%02X (expecting 0xBD)", byte(pes.StartCodeHeader.StreamID()))
		return
	}
	//// Read PES packet length
	if _, err = fd.ReadAt(pes.PacketLength[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES Packet Lenght header: %w", err)
		return
	}
	cursor += int64(len(pes.PacketLength))
	//// 0xBD stream type has PES header extension, read it
	pes.Extension = new(PESExtension)
	if _, err = fd.ReadAt(pes.Extension[:], cursor); err != nil {
		err = fmt.Errorf("failed to read PES extension header: %w", err)
		return
	}
	cursor += int64(len(*pes.Extension))
	//// Read PES Extension Data
	pes.ExtensionData = make([]byte, pes.Extension.RemainingHeaderLength())
	if _, err = fd.ReadAt(pes.ExtensionData, cursor); err != nil {
		err = fmt.Errorf("failed to read PES extension data: %w", err)
		return
	}
	cursor += int64(len(pes.ExtensionData))
	//// Read sub stream id for private streams (we are one: 0xBD)
	if _, err = fd.ReadAt(pes.SubStreamID[:], cursor); err != nil {
		err = fmt.Errorf("failed to read sub stream id: %w", err)
		return
	}
	cursor += int64(len(pes.SubStreamID))
	//// Headers done
	fmt.Println(pes.String())
	fmt.Println(pes.GoString())
	// Payload
	buffer := make([]byte, 128)
	if _, err = fd.ReadAt(buffer, cursor); err != nil {
		err = fmt.Errorf("failed to read payload beginning: %w", err)
		return
	}
	fmt.Printf("Payload beginning: %08b\n", buffer)
	//// DEBUG
	return
}
