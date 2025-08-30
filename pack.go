package vobsub

import (
	"fmt"
	"time"
)

// https://dvd.sourceforge.net/dvdinfo/packhdr.html

type PackHeader struct {
	StartCodeHeader StartCodeHeader
	Remaining       [10]byte
}

func (ph PackHeader) Len() int {
	return len(ph.StartCodeHeader) + len(ph.Remaining)
}

func (ph PackHeader) Validate() error {
	if err := ph.StartCodeHeader.Validate(); err != nil {
		return err
	}
	// Validate the PACK identifier
	if ph.StartCodeHeader[3] != StreamIDPackHeader {
		return fmt.Errorf("invalid PACK identifier: %08b (expected %08b)", ph.StartCodeHeader[3], StreamIDPackHeader)
	}
	// Check for fixed bits in the SCR 6 first bytes
	if ph.Remaining[0]>>6 != 0b01 {
		return fmt.Errorf("invalid SCR 1st fixed bits: %02b (expected 0b01)", ph.Remaining[0]>>6)
	}
	if (ph.Remaining[0]&0b00000100)>>2 != 0b1 {
		return fmt.Errorf("invalid SCR 2nd fixed bits: %b (expected 0b1)", (ph.Remaining[0]&0b00000100)>>2)
	}
	if (ph.Remaining[2]&0b00000100)>>2 != 0b1 {
		return fmt.Errorf("invalid SCR 3rd fixed bits: %b (expected 0b1)", (ph.Remaining[2]&0b00000100)>>2)
	}
	if (ph.Remaining[4]&0b00000100)>>2 != 0b1 {
		return fmt.Errorf("invalid SCR 4th fixed bits: %b (expected 0b1)", (ph.Remaining[4]&0b00000100)>>2)
	}
	if ph.Remaining[5]&0b00000001 != 0b1 {
		return fmt.Errorf("invalid SCR 5th fixed bits: %b (expected 0b1)", ph.Remaining[5]&0b00000001)
	}
	// Check for fixed bits in the last 4 bytes
	if ph.Remaining[8]&0b00000011 != 0b11 {
		return fmt.Errorf("invalid SCR 5th fixed bits: %02b (expected 0b11)", ph.Remaining[8]&0b00000011)
	}
	// ProgramMuxRate can not be 0
	if ph.ProgramMuxRate() == 0 {
		return fmt.Errorf("program mux rate cannot be 0")
	}
	return nil
}

func (ph PackHeader) SystemClockReferenceRaw() (quotient uint64, remainder uint64) {
	// Extract the quotient
	quotient = uint64(ph.Remaining[0]&0b00111000)<<(30-3) | uint64(ph.Remaining[0]&0b00000011)<<28
	quotient |= uint64(ph.Remaining[1]) << 20
	quotient |= uint64(ph.Remaining[2]&0b11111000)<<(15-3) | uint64(ph.Remaining[2]&0b00000011)<<13
	quotient |= uint64(ph.Remaining[3]) << 5
	quotient |= uint64(ph.Remaining[4]) >> 3
	// Extract the remainder
	remainder = uint64(ph.Remaining[4]&0b00000011) << 7
	remainder |= uint64(ph.Remaining[5]) >> 1
	return
}

func (ph PackHeader) SystemClockReference() time.Duration {
	return ComputeSCRTiming(ph.SystemClockReferenceRaw())
}

func (ph PackHeader) ProgramMuxRate() uint64 {
	return uint64(ph.Remaining[6])<<(16-2) | uint64(ph.Remaining[7])<<(8-2) | uint64(ph.Remaining[8])>>2
}

func (ph PackHeader) StuffingBytesLength() int64 {
	return int64(ph.Remaining[9] & 0b00000111)
}

func (ph PackHeader) String() string {
	return fmt.Sprintf("PackHeader{%s, SCR: %s, ProgramMuxRate: %d, StuffingBytesLength: %d}",
		ph.StartCodeHeader, ph.SystemClockReference(), ph.ProgramMuxRate(), ph.StuffingBytesLength(),
	)
}

func (ph PackHeader) GoString() string {
	return fmt.Sprintf("PackHeader{%s  PackHeader{%08b %08b %08b %08b %08b %08b  %08b %08b %08b  %08b}}",
		ph.StartCodeHeader.GoString(),
		ph.Remaining[0], ph.Remaining[1], ph.Remaining[2], ph.Remaining[3], ph.Remaining[4], ph.Remaining[5],
		ph.Remaining[6], ph.Remaining[7], ph.Remaining[8], ph.Remaining[9],
	)
}

const (
	MaxSCRValue          = (1 << 33) - 1 // 33-bit maximum
	MaxSCRExtValue       = (1 << 9) - 1  // 9-bit maximum
	SCRClockFrequency    = 27_000_000    // 27 MHz clock frequency used for SCR
	PTSDTSClockFrequency = 90_000        // 90 kHz clock frequency for PTS and DTS
)

func ComputeSCRTiming(quotient uint64, remainder uint64) time.Duration {
	if quotient > MaxSCRValue || remainder > MaxSCRExtValue {
		return 0
	}
	totalTicks := quotient*(SCRClockFrequency/PTSDTSClockFrequency) + uint64(remainder)
	return time.Duration(totalTicks * uint64(time.Second) / SCRClockFrequency)
}
