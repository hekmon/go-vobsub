package vobsub

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Decode reads a sub file and its associated idx file to extract and generate its embedded subtitles images.
// As .sub files can contains multilples streams, the returned map contains all streams with their ID as key.
// Most of sub files only contains one stream (ID 0).
func Decode(subFile string, fullSizeImages bool) (subtitles map[int][]Subtitle, skippedBadSub []error, err error) {
	// Verify and prepare files path
	extension := filepath.Ext(subFile)
	if extension != ".sub" {
		err = fmt.Errorf("expected .sub file extension: got %q", extension)
		return
	}
	idxFile := subFile[:len(subFile)-len(extension)] + ".idx"
	// Parse Idx file to get subtitle metadata
	metadata, err := ReadIdxFile(idxFile)
	if err != nil {
		err = fmt.Errorf("failed to read .idx file: %w", err)
		return
	}
	// Parse Sub
	privateStream1Packets, err := ReadSubFile(subFile)
	if err != nil {
		err = fmt.Errorf("failed to read .sub file: %w", err)
		return
	}
	// Concat splitted packets
	subtitlesPackets := make([]PESPacket, 0, len(privateStream1Packets))
	for _, pkt := range privateStream1Packets {
		if pkt.Header.Extension.Data.ComputePTS() != 0 {
			// New subtitle
			subtitlesPackets = append(subtitlesPackets, pkt)
		} else {
			// Subtitle has been split in multiples packets, concat to current sub
			currentSub := subtitlesPackets[len(subtitlesPackets)-1]
			currentSub.Payload = append(currentSub.Payload, pkt.Payload...)
			subtitlesPackets[len(subtitlesPackets)-1] = currentSub
		}
	}
	// Decode raw subtitles to final subtitles
	subtitles = make(map[int][]Subtitle, 1)
	var (
		rawSub     SubtitleRaw
		pts        time.Duration
		streamSubs []Subtitle
		found      bool
		startDelay time.Duration
		stopDelay  time.Duration
		subImg     image.Image
	)
	for index, subPkt := range subtitlesPackets {
		// Recover the current stream subs slice
		if streamSubs, found = subtitles[subPkt.Header.SubStreamID.SubtitleID()]; !found {
			streamSubs = make([]Subtitle, 0, len(subtitlesPackets))
		}
		// Extract raw subtitle from packet
		if rawSub, err = subPkt.ExtractSubtitle(); err != nil {
			// Encountered some bad packets in the wild: discarding them
			// I compared with Subtitle Edit nothing was missing, it seems SE did skip them too
			skippedBadSub = append(skippedBadSub, fmt.Errorf("packet #%d: %w", index+1, err))
			err = nil
			continue
		}
		// Generate the image
		if subImg, startDelay, stopDelay, err = rawSub.Decode(metadata, fullSizeImages); err != nil {
			err = fmt.Errorf("failed to decode subtitle: %w", err)
			return
		}
		// Create the final subtitle
		pts = subPkt.Header.Extension.Data.ComputePTS()
		streamSubs = append(streamSubs, Subtitle{
			Start: metadata.TimeOffset + pts + startDelay,
			Stop:  metadata.TimeOffset + pts + stopDelay,
			Image: subImg,
		})
		// Save the slice with the new sub batch to its stream
		subtitles[subPkt.Header.SubStreamID.SubtitleID()] = streamSubs
	}
	// Security check: some (rare) subtitles do not have stopDate, resulting in a stopDelay at 0 and so a 0 duration
	// To fix this we will be using the next subtitle start date and remove 100 milliseconds to compute a stop value
	// different from the start value thus allowing the subtitle to be shown
	for _, streamSubs := range subtitles {
		for index, sub := range streamSubs {
			if sub.Start == sub.Stop {
				if index+1 < len(subtitles) {
					potentialStop := streamSubs[index+1].Start - 100*time.Millisecond
					if potentialStop > sub.Start {
						sub.Stop = potentialStop
						streamSubs[index] = sub
					} // else nothing we can do (it might work with less than 100ms but it won't be readable either way[too fast])
				} // else nothing we can do here
			}
		}
	}
	return
}

// ReadIdxFile reads the idx file and returns its metadata.
func ReadIdxFile(Idxfile string) (metadata IdxMetadata, err error) {
	// Open the binary sub file
	fd, err := os.Open(Idxfile)
	if err != nil {
		err = fmt.Errorf("failed to open file: %w", err)
		return
	}
	defer fd.Close()
	// Parse its metadata
	if metadata, err = parseIdx(fd); err != nil {
		err = fmt.Errorf("failed to parse Idx metadata file: %w", err)
		return
	}
	return
}

// ReadSubFile reads the sub file and returns its packets.
func ReadSubFile(subFile string) (privateStream1Packets []PESPacket, err error) {
	// Open the binary sub file
	fd, err := os.Open(subFile)
	if err != nil {
		err = fmt.Errorf("failed to open file: %w", err)
		return
	}
	defer fd.Close()
	// Parse its packets
	var (
		nextAt int64
		packet PESPacket
	)
	for nextAt != stopFlagValue {
		if packet, nextAt, err = parsePacket(fd, nextAt); err != nil {
			err = fmt.Errorf("failed to parse packet: %w", err)
			return
		}
		if packet.Header.MPH.StreamID() == StreamIDPrivateStream1 {
			privateStream1Packets = append(privateStream1Packets, packet)
		}
	}
	return
}

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
		err = fmt.Errorf("failed to read PES Packet Length header: %w", err)
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
		err = fmt.Errorf("failed to read PES Packet Length header: %w", err)
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
